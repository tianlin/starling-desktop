package store

import (
	"path/filepath"
	"testing"
)

func TestPersistenceAndScopes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Put("account-a", "queue", "main", []string{"one", "two"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Put("guest", "queue", "main", []string{"guest"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var got []string
	ok, err := s.Get("account-a", "queue", "main", &got)
	if err != nil || !ok || len(got) != 2 {
		t.Fatalf("%v %v %v", got, ok, err)
	}
	if err = s.DeleteScope("account-a"); err != nil {
		t.Fatal(err)
	}
	ok, err = s.Get("account-a", "queue", "main", &got)
	if err != nil || ok {
		t.Fatal("private data retained")
	}
	ok, err = s.Get("guest", "queue", "main", &got)
	if err != nil || !ok || got[0] != "guest" {
		t.Fatal("guest data removed")
	}
}
func TestBoundParameters(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	scope := "x'; DROP TABLE kv;--"
	if e = s.Put(scope, "bookmarks", "x", map[string]string{"title": "quote ' 中文"}); e != nil {
		t.Fatal(e)
	}
	var v map[string]string
	if _, e = s.Get(scope, "bookmarks", "x", &v); e != nil || v["title"] != "quote ' 中文" {
		t.Fatalf("%v %v", v, e)
	}
}
func TestDeleteKindDoesNotClearProgress(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.Put("a", "cache", "f", 1)
	s.Put("a", "progress", "e", 20)
	if e = s.DeleteKind("a", "cache"); e != nil {
		t.Fatal(e)
	}
	var n int
	ok, e := s.Get("a", "progress", "e", &n)
	if e != nil || !ok || n != 20 {
		t.Fatal(n, e)
	}
}
func TestAtomicReplacement(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.Put("a", "cache", "f", []int{1, 2, 3})
	s.Put("a", "cache", "f", []int{4})
	var n []int
	s.Get("a", "cache", "f", &n)
	if len(n) != 1 || n[0] != 4 {
		t.Fatal(n)
	}
}
func TestBadDatabaseDoesNotOverwrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	if e := writeBadDatabase(p); e != nil {
		t.Fatal(e)
	}
	if s, e := Open(p); e == nil {
		s.Close()
		t.Fatal("accepted invalid database")
	}
}
