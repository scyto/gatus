package sql

import (
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/TwiN/gatus/v5/storage"
)

func TestStore_Backup(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore("sqlite", dir+"/TestStore_Backup.db", false, storage.DefaultMaximumNumberOfResults, storage.DefaultMaximumNumberOfEvents)
	if err != nil {
		t.Fatal("expected no error, got", err.Error())
	}
	defer store.Close()
	if err := store.InsertEndpointResult(&testEndpoint, &testSuccessfulResult); err != nil {
		t.Fatal("expected no error, got", err.Error())
	}
	backupPath := dir + "/backup/gatus.db"
	if err := os.MkdirAll(dir+"/backup", 0o755); err != nil {
		t.Fatal(err)
	}
	// Twice, so the second run replaces the first backup and a leftover .tmp is not a problem.
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(backupPath+".tmp", []byte("leftover"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := store.Backup(backupPath); err != nil {
			t.Fatal("expected no error, got", err.Error())
		}
	}
	if _, err := os.Stat(backupPath + ".tmp"); !os.IsNotExist(err) {
		t.Error("expected the temporary file to be gone after a backup")
	}
	backup, err := sql.Open("sqlite", backupPath)
	if err != nil {
		t.Fatal("expected no error opening the backup, got", err.Error())
	}
	defer backup.Close()
	var integrity string
	if err := backup.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("expected integrity_check ok, got %q (%v)", integrity, err)
	}
	var endpoints int
	if err := backup.QueryRow("SELECT COUNT(*) FROM endpoints").Scan(&endpoints); err != nil || endpoints != 1 {
		t.Fatalf("expected 1 endpoint in the backup, got %d (%v)", endpoints, err)
	}
}

func TestStore_BackupCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore("sqlite", dir+"/TestStore_BackupCreatesDirectory.db", false, storage.DefaultMaximumNumberOfResults, storage.DefaultMaximumNumberOfEvents)
	defer store.Close()
	if err := store.Backup(dir + "/not/yet/there/gatus.db"); err != nil {
		t.Fatal("expected no error, got", err.Error())
	}
	if _, err := os.Stat(dir + "/not/yet/there/gatus.db"); err != nil {
		t.Error("expected the backup in a directory Backup created, got", err)
	}
}

func TestStore_BackupRefusesTheDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/TestStore_BackupRefusesTheDatabase.db"
	store, _ := NewStore("sqlite", dbPath, false, storage.DefaultMaximumNumberOfResults, storage.DefaultMaximumNumberOfEvents)
	defer store.Close()
	if err := os.Symlink(dbPath, dir+"/link.db"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm", dir + "/./TestStore_BackupRefusesTheDatabase.db", dir + "/link.db"} {
		if err := store.Backup(path); !errors.Is(err, ErrBackupPathIsDatabase) {
			t.Errorf("Backup(%s): expected ErrBackupPathIsDatabase, got %v", path, err)
		}
	}
	if err := store.InsertEndpointResult(&testEndpoint, &testSuccessfulResult); err != nil {
		t.Error("expected the database to still work, got", err.Error())
	}
}

func TestStore_BackupRefusesTheDatabaseOpenedByURI(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/TestStore_BackupRefusesTheDatabaseOpenedByURI.db"
	store, err := NewStore("sqlite", "file:"+dbPath+"?mode=rwc", false, storage.DefaultMaximumNumberOfResults, storage.DefaultMaximumNumberOfEvents)
	if err != nil {
		t.Fatal("expected no error, got", err.Error())
	}
	defer store.Close()
	for _, path := range []string{dbPath, dbPath + "-wal"} {
		if err := store.Backup(path); !errors.Is(err, ErrBackupPathIsDatabase) {
			t.Errorf("Backup(%s): expected ErrBackupPathIsDatabase, got %v", path, err)
		}
	}
	if err := store.Backup(dir + "/backup/gatus.db"); err != nil {
		t.Error("expected a backup elsewhere to work, got", err)
	}
}

func TestSQLiteFilePath(t *testing.T) {
	scenarios := map[string]string{
		"/data/gatus.db":                      "/data/gatus.db",
		"data/gatus.db":                       "data/gatus.db",
		"file:/data/gatus.db?mode=rwc":        "/data/gatus.db",
		"file:///data/gatus.db":               "/data/gatus.db",
		"file://localhost/data/gatus.db?x=1":  "/data/gatus.db",
		"file:data/gatus.db":                  "data/gatus.db",
		"file:/data/my%20gatus.db?cache=priv": "/data/my gatus.db",
	}
	for in, expected := range scenarios {
		if actual := sqliteFilePath(in); actual != expected {
			t.Errorf("sqliteFilePath(%s): expected %s, got %s", in, expected, actual)
		}
	}
}

func TestStore_BackupWithBlankPath(t *testing.T) {
	store, _ := NewStore("sqlite", t.TempDir()+"/TestStore_BackupWithBlankPath.db", false, storage.DefaultMaximumNumberOfResults, storage.DefaultMaximumNumberOfEvents)
	defer store.Close()
	if err := store.Backup(""); !errors.Is(err, ErrBackupPathNotSpecified) {
		t.Error("expected ErrBackupPathNotSpecified, got", err)
	}
}

func TestStore_StartSQLiteBackup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GATUS_SQLITE_BACKUP_PATH", dir+"/started.db")
	store, err := NewStore("sqlite", dir+"/TestStore_StartSQLiteBackup.db", false, storage.DefaultMaximumNumberOfResults, storage.DefaultMaximumNumberOfEvents)
	if err != nil {
		t.Fatal("expected no error, got", err.Error())
	}
	// The first backup runs as soon as the store starts.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(dir + "/started.db"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expected a backup soon after the store started")
		}
		time.Sleep(50 * time.Millisecond)
	}
	store.Close()
	if store.stopBackup == nil {
		t.Error("expected the backup to have a stop function")
	}
	// Close waits for the backup to stop.
	select {
	case <-store.backupDone:
	default:
		t.Error("expected the backup to have stopped once Close returned")
	}
}

func TestUntilMinute(t *testing.T) {
	scenarios := []struct {
		now      string
		minute   int
		expected time.Duration
	}{
		{"2026-09-25T13:10:00Z", 50, 40 * time.Minute},
		{"2026-09-25T13:50:00Z", 50, time.Hour},
		{"2026-09-25T13:55:30Z", 50, 54*time.Minute + 30*time.Second},
		{"2026-09-25T13:00:00Z", 0, time.Hour},
	}
	for _, scenario := range scenarios {
		now, _ := time.Parse(time.RFC3339, scenario.now)
		if actual := untilMinute(now, scenario.minute); actual != scenario.expected {
			t.Errorf("untilMinute(%s, %d): expected %s, got %s", scenario.now, scenario.minute, scenario.expected, actual)
		}
	}
	// A zone with a half-hour offset: the minute is the local wall-clock minute.
	india := time.FixedZone("IST", 5*60*60+30*60)
	now := time.Date(2026, 9, 25, 13, 10, 0, 0, india)
	if actual := untilMinute(now, 50); actual != 40*time.Minute {
		t.Errorf("untilMinute(13:10 IST, 50): expected 40m0s, got %s", actual)
	}
	// Daylight saving changes: the next time the clock shows the minute, not an hour on.
	dstScenarios := []struct {
		zone     string
		nowUTC   string
		expected time.Duration
	}{
		// Lord Howe springs forward half an hour, 02:00 to 02:30: 01:55 to 02:50 is 25 minutes.
		{"Australia/Lord_Howe", "2026-10-03T15:25:00Z", 25 * time.Minute},
		// Los Angeles falls back, 02:00 PDT to 01:00 PST: 01:55 PDT to 01:50 PST is 55 minutes.
		{"America/Los_Angeles", "2026-11-01T08:55:00Z", 55 * time.Minute},
	}
	for _, scenario := range dstScenarios {
		location, err := time.LoadLocation(scenario.zone)
		if err != nil {
			t.Skipf("no time zone data for %s: %s", scenario.zone, err)
		}
		now, _ := time.Parse(time.RFC3339, scenario.nowUTC)
		now = now.In(location)
		if actual := untilMinute(now, 50); actual != scenario.expected {
			t.Errorf("untilMinute(%s, 50): expected %s, got %s", now, scenario.expected, actual)
		}
	}
}
