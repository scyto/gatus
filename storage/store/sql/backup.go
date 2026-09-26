package sql

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TwiN/logr"
)

const defaultBackupMinute = 50

var (
	// ErrBackupPathNotSpecified is the error returned when Backup is called with a blank path
	ErrBackupPathNotSpecified = errors.New("backup path cannot be empty")

	// ErrBackupPathIsDatabase is the error returned when the backup path is the database itself
	ErrBackupPathIsDatabase = errors.New("backup path cannot be the database or one of its -wal, -shm or -journal files")
)

// startSQLiteBackup starts writing a copy of the SQLite database to the path in
// GATUS_SQLITE_BACKUP_PATH: once at start, then every hour at the minute in
// GATUS_SQLITE_BACKUP_MINUTE (default 50). It does nothing if the path is unset.
func (s *Store) startSQLiteBackup() {
	path := os.Getenv("GATUS_SQLITE_BACKUP_PATH")
	if len(path) == 0 {
		return
	}
	minute := defaultBackupMinute
	if value := os.Getenv("GATUS_SQLITE_BACKUP_MINUTE"); len(value) > 0 {
		if m, err := strconv.Atoi(value); err == nil && m >= 0 && m < 60 {
			minute = m
		} else {
			logr.Warnf("[sql.startSQLiteBackup] Invalid GATUS_SQLITE_BACKUP_MINUTE=%s, using %d", value, defaultBackupMinute)
		}
	}
	var ctx context.Context
	ctx, s.stopBackup = context.WithCancel(context.Background())
	go s.backupEveryHour(ctx, path, minute)
}

func (s *Store) backupEveryHour(ctx context.Context, path string, minute int) {
	for {
		if err := s.Backup(path); err != nil {
			logr.Errorf("[sql.backupEveryHour] Failed to back up the database to %s: %s", path, err.Error())
		} else {
			logr.Infof("[sql.backupEveryHour] Backed up the database to %s", path)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(untilMinute(time.Now(), minute)):
		}
	}
}

// Backup writes a consistent copy of the SQLite database to path with VACUUM INTO,
// creating path's directory if needed. The copy is written to path+".tmp" and
// renamed to path only once it is complete.
func (s *Store) Backup(path string) error {
	if len(path) == 0 {
		return ErrBackupPathNotSpecified
	}
	if s.isDatabaseFile(path) {
		return ErrBackupPathIsDatabase
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := s.db.Exec("VACUUM INTO ?", tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// isDatabaseFile reports whether path is the store's database file, or one of the
// files SQLite keeps beside it, by name or as the same file.
func (s *Store) isDatabaseFile(path string) bool {
	database := sqliteFilePath(s.path)
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if sameFile(path, database+suffix) || sameFile(path+".tmp", database+suffix) {
			return true
		}
	}
	return false
}

// sqliteFilePath returns the file behind a SQLite path, which may be a URI such as
// file:/data/gatus.db?mode=rwc or file:///data/gatus.db.
func sqliteFilePath(path string) string {
	if !strings.HasPrefix(path, "file:") {
		return path
	}
	rest := strings.TrimPrefix(path, "file:")
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		rest = rest[:i]
	}
	if strings.HasPrefix(rest, "//") {
		// file://host/path: the host is empty or localhost for a local file
		rest = rest[2:]
		if i := strings.Index(rest, "/"); i >= 0 {
			rest = rest[i:]
		}
	}
	if unescaped, err := url.PathUnescape(rest); err == nil {
		rest = unescaped
	}
	return rest
}

func sameFile(a, b string) bool {
	if absA, errA := filepath.Abs(a); errA == nil {
		if absB, errB := filepath.Abs(b); errB == nil && absA == absB {
			return true
		}
	}
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(infoA, infoB)
}

// untilMinute returns the time from now until the next time the minute past the hour is minute.
func untilMinute(now time.Time, minute int) time.Duration {
	next := now.Truncate(time.Hour).Add(time.Duration(minute) * time.Minute)
	if !next.After(now) {
		next = next.Add(time.Hour)
	}
	return next.Sub(now)
}
