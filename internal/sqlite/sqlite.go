// Package sqlite is a deliberately small, parameterized SQLite binding.
// Windows uses Microsoft's system winsqlite3.dll. Linux uses libsqlite3 for tests.
package sqlite

import (
	"errors"
	"sync"
)

type DB struct {
	mu     sync.Mutex
	handle nativeHandle
	closed bool
}

func Open(path string) (*DB, error) {
	h, e := nativeOpen(path)
	if e != nil {
		return nil, e
	}
	return &DB{handle: h}, nil
}
func (d *DB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	e := nativeClose(d.handle)
	if e == nil {
		d.closed = true
	}
	return e
}
func (d *DB) Exec(query string, args ...string) error { _, e := d.Query(query, args...); return e }
func (d *DB) Query(query string, args ...string) ([][]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, errors.New("sqlite: closed")
	}
	return nativeQuery(d.handle, query, args)
}
