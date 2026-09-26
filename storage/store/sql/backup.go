package sql

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/TwiN/logr"
)

const defaultBackupMinute = 50

// ErrBackupPathNotSpecified is the error returned when Backup is called with a blank path
var ErrBackupPathNotSpecified = errors.New("backup path cannot be empty")

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

// untilMinute returns the time from now until the next time the minute past the hour is minute.
func untilMinute(now time.Time, minute int) time.Duration {
	next := now.Truncate(time.Hour).Add(time.Duration(minute) * time.Minute)
	if !next.After(now) {
		next = next.Add(time.Hour)
	}
	return next.Sub(now)
}
