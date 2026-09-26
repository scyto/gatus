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
}
