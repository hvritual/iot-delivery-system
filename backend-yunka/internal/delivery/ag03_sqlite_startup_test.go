package delivery

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"modernc.org/sqlite"
)

// Exercise the public constructor against a real independently held file lock.
// The existing busy budget must cover startup pragmas, not only later writes.
func TestAG03SQLiteStartupWaitsForExternalLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "startup.db")
	locker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Close()
	locker.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := locker.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "CREATE TABLE ag03_startup_marker(value TEXT); INSERT INTO ag03_startup_marker VALUES ('preserved')"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	type opened struct {
		repository *SQLiteRepository
		err        error
	}
	results := make(chan opened, 1)
	started := make(chan struct{})
	go func() { close(started); r, err := NewSQLiteRepository(path); results <- opened{r, err} }()
	<-started
	var result opened
	received, premature := false, false
	defer func() {
		if !received {
			if locked {
				_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
				locked = false
			}
			select {
			case result = <-results:
			case <-time.After(7 * time.Second):
				t.Error("constructor cleanup did not finish")
				return
			}
		}
		if result.repository != nil {
			_ = result.repository.Close()
		}
	}()
	select {
	case result = <-results:
		received, premature = true, true
	case <-time.After(200 * time.Millisecond):
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		t.Fatal(err)
	}
	locked = false
	if premature {
		var sqliteError *sqlite.Error
		if errors.As(result.err, &sqliteError) && sqliteError.Code() == 5 && strings.Contains(result.err.Error(), "configure SQLite connection") {
			t.Fatalf("AG03_STARTUP_BUSY_BEFORE_RELEASE: %v", result.err)
		}
		t.Fatalf("constructor completed while external lock was held: %v", result.err)
	}
	select {
	case result = <-results:
		received = true
	case <-time.After(6 * time.Second):
		t.Fatal("constructor exceeded existing busy budget after release")
	}
	if result.err != nil {
		t.Fatalf("startup after external lock release: %v", result.err)
	}
	if result.repository == nil {
		t.Fatal("missing repository")
	}
	for _, check := range []struct{ query, want string }{
		{"PRAGMA journal_mode", "wal"}, {"PRAGMA busy_timeout", "5000"},
		{"PRAGMA foreign_keys", "1"}, {"SELECT value FROM ag03_startup_marker", "preserved"},
	} {
		var got string
		if err := result.repository.Database().QueryRowContext(ctx, check.query).Scan(&got); err != nil || got != check.want {
			t.Fatalf("%s = %q err=%v, want %q", check.query, got, err, check.want)
		}
	}
}
