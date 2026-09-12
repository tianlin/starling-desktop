//go:build (linux || darwin) && cgo

package sqlite

/*
#cgo LDFLAGS: -lsqlite3
#include <sqlite3.h>
#include <stdlib.h>
static int bind_text_copy(sqlite3_stmt *s, int n, const char *v, int len) {
 return sqlite3_bind_text(s,n,v,len,SQLITE_TRANSIENT);
}
*/
import "C"
import (
	"errors"
	"unsafe"
)

type nativeHandle = *C.sqlite3

func nativeOpen(path string) (nativeHandle, error) {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	var d *C.sqlite3
	rc := C.sqlite3_open_v2(p, &d, C.SQLITE_OPEN_READWRITE|C.SQLITE_OPEN_CREATE|C.SQLITE_OPEN_FULLMUTEX, nil)
	if rc != C.SQLITE_OK {
		if d != nil {
			C.sqlite3_close(d)
		}
		return nil, errors.New("sqlite: cannot open database")
	}
	C.sqlite3_busy_timeout(d, 5000)
	return d, nil
}
func nativeClose(h nativeHandle) error {
	if C.sqlite3_close(h) != C.SQLITE_OK {
		return errors.New("sqlite: close failed")
	}
	return nil
}
func nativeQuery(h nativeHandle, q string, args []string) ([][]string, error) {
	d := h
	sql := C.CString(q)
	defer C.free(unsafe.Pointer(sql))
	var s *C.sqlite3_stmt
	if C.sqlite3_prepare_v2(d, sql, -1, &s, nil) != C.SQLITE_OK {
		return nil, errors.New("sqlite: prepare failed")
	}
	defer C.sqlite3_finalize(s)
	if int(C.sqlite3_bind_parameter_count(s)) != len(args) {
		return nil, errors.New("sqlite: parameter count mismatch")
	}
	for i, a := range args {
		v := C.CString(a)
		rc := C.bind_text_copy(s, C.int(i+1), v, C.int(len(a)))
		C.free(unsafe.Pointer(v))
		if rc != C.SQLITE_OK {
			return nil, errors.New("sqlite: bind failed")
		}
	}
	rows := make([][]string, 0)
	for {
		rc := C.sqlite3_step(s)
		if rc == C.SQLITE_DONE {
			return rows, nil
		}
		if rc != C.SQLITE_ROW {
			return nil, errors.New("sqlite: step failed")
		}
		row := make([]string, int(C.sqlite3_column_count(s)))
		for i := range row {
			p := C.sqlite3_column_text(s, C.int(i))
			n := C.sqlite3_column_bytes(s, C.int(i))
			if n > 16<<20 || n < 0 {
				return nil, errors.New("sqlite: column exceeds limit")
			}
			if p != nil {
				row[i] = C.GoStringN((*C.char)(unsafe.Pointer(p)), n)
			}
		}
		rows = append(rows, row)
	}
}
