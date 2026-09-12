package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireValue(t *testing.T, s *Store, expected string) {
	t.Helper()
	var got string
	ok, err := s.Get("guest", "progress", "episode", &got)
	if err != nil || !ok || got != expected {
		t.Fatalf("progress=%q found=%v err=%v", got, ok, err)
	}
}

func TestUnicodePathReopen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "中文目录 空格", "收听.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Put("guest", "progress", "episode", "42秒"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	requireValue(t, s, "42秒")
}

func TestFullDatabasePreservesPreviousValueAndRecovers(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "full.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Put("guest", "progress", "episode", "42"); err != nil {
		t.Fatal(err)
	}
	pages, err := s.db.Query("PRAGMA page_count")
	if err != nil {
		t.Fatal(err)
	}
	// SQLite's page limit produces SQLITE_FULL without filling the user's disk.
	if err = s.db.Exec("PRAGMA max_page_count=" + pages[0][0]); err != nil {
		t.Fatal(err)
	}
	if err = s.Put("guest", "progress", "episode", strings.Repeat("x", 256<<10)); err == nil {
		t.Fatal("full write was accepted")
	}
	requireValue(t, s, "42")
	if err = s.db.Exec("PRAGMA max_page_count=1000"); err != nil {
		t.Fatal(err)
	}
	if err = s.Put("guest", "progress", "episode", "43"); err != nil {
		t.Fatal(err)
	}
	requireValue(t, s, "43")
}

func TestLockedDatabasePreservesValueAndRecovers(t *testing.T) {
	p := filepath.Join(t.TempDir(), "locked.db")
	a, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err = a.Put("guest", "progress", "episode", "42"); err != nil {
		t.Fatal(err)
	}
	if err = b.db.Exec("PRAGMA busy_timeout=20"); err != nil {
		t.Fatal(err)
	}
	if err = a.db.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer a.db.Exec("ROLLBACK")
	if err = b.Put("guest", "progress", "episode", "lost"); err == nil {
		t.Fatal("locked write was accepted")
	}
	if err = a.db.Exec("ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	requireValue(t, b, "42")
	if err = b.Put("guest", "progress", "episode", "43"); err != nil {
		t.Fatal(err)
	}
	requireValue(t, a, "43")
}

func TestAbruptExitRecoversCommittedProgress(t *testing.T) {
	if p := os.Getenv("STARLING_STORE_CRASH_FIXTURE"); p != "" {
		s, err := Open(p)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Put("guest", "progress", "episode", "committed"); err != nil {
			t.Fatal(err)
		}
		if err = s.db.Exec("BEGIN IMMEDIATE"); err != nil {
			t.Fatal(err)
		}
		if err = s.Put("guest", "progress", "episode", "uncommitted"); err != nil {
			t.Fatal(err)
		}
		os.Exit(23) // Only this isolated test child exits, without Close or rollback.
	}
	p := filepath.Join(t.TempDir(), "crash.db")
	cmd := exec.Command(os.Args[0], "-test.run=^TestAbruptExitRecoversCommittedProgress$")
	hideTestChild(cmd)
	cmd.Env = append(os.Environ(), "STARLING_STORE_CRASH_FIXTURE="+p)
	output, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 23 {
		t.Fatalf("child=%v output=%s", err, output)
	}
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	requireValue(t, s, "committed")
	if err = s.Put("guest", "progress", "episode", "recovered"); err != nil {
		t.Fatal(err)
	}
	requireValue(t, s, "recovered")
}
