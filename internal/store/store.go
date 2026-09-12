// Package store keeps metadata only; credentials are never written here.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"starling/internal/sqlite"
)

type Store struct{ db *sqlite.DB }

func Open(path string) (*Store, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	d, e := sqlite.Open(path)
	if e != nil {
		return nil, e
	}
	fail := func(e error) (*Store, error) { d.Close(); return nil, e }
	rows, e := d.Query("PRAGMA user_version")
	if e != nil {
		return fail(e)
	}
	if len(rows) != 1 || len(rows[0]) != 1 || (rows[0][0] != "0" && rows[0][0] != "1") {
		return fail(errors.New("store: unsupported schema version; original database preserved"))
	}
	for _, q := range []string{"PRAGMA journal_mode=DELETE", "PRAGMA secure_delete=ON", "PRAGMA synchronous=FULL", "CREATE TABLE IF NOT EXISTS kv (scope TEXT NOT NULL, kind TEXT NOT NULL, k TEXT NOT NULL, v TEXT NOT NULL, PRIMARY KEY(scope,kind,k))", "PRAGMA user_version=1"} {
		if e = d.Exec(q); e != nil {
			return fail(e)
		}
	}
	_ = os.Chmod(path, 0600)
	return &Store{db: d}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Put(scope, kind, key string, value any) error {
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	if len(b) > 16<<20 {
		return errors.New("store: value too large")
	}
	return s.db.Exec("INSERT INTO kv(scope,kind,k,v) VALUES(?,?,?,?) ON CONFLICT(scope,kind,k) DO UPDATE SET v=excluded.v", scope, kind, key, string(b))
}
func (s *Store) Get(scope, kind, key string, dst any) (bool, error) {
	rows, e := s.db.Query("SELECT v FROM kv WHERE scope=? AND kind=? AND k=?", scope, kind, key)
	if e != nil || len(rows) == 0 {
		return false, e
	}
	if e = json.Unmarshal([]byte(rows[0][0]), dst); e != nil {
		return false, errors.New("store: invalid persisted data")
	}
	return true, nil
}
func (s *Store) Delete(scope, kind, key string) error {
	return s.db.Exec("DELETE FROM kv WHERE scope=? AND kind=? AND k=?", scope, kind, key)
}
func (s *Store) DeleteScope(scope string) error {
	return s.db.Exec("DELETE FROM kv WHERE scope=?", scope)
}
func (s *Store) DeleteKind(scope, kind string) error {
	return s.db.Exec("DELETE FROM kv WHERE scope=? AND kind=?", scope, kind)
}
func (s *Store) Clear() error { return s.db.Exec("DELETE FROM kv") }
func (s *Store) List(scope, kind string) ([]json.RawMessage, error) {
	rows, e := s.db.Query("SELECT v FROM kv WHERE scope=? AND kind=? ORDER BY k", scope, kind)
	if e != nil {
		return nil, e
	}
	out := make([]json.RawMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, json.RawMessage(r[0]))
	}
	return out, nil
}
