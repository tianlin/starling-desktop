//go:build windows

package sqlite

import (
	"errors"
	"runtime"
	"starling/internal/winapi"
	"syscall"
	"unsafe"
)

type nativeHandle = uintptr

var copyNativeMemory = winapi.SystemDLL("ntdll.dll").NewProc("RtlMoveMemory")

var sqliteDLL = winapi.SystemDLL("winsqlite3.dll")
var sqliteOpen = sqliteDLL.NewProc("sqlite3_open_v2")
var sqliteClose = sqliteDLL.NewProc("sqlite3_close")
var sqlitePrepare = sqliteDLL.NewProc("sqlite3_prepare_v2")
var sqliteFinalize = sqliteDLL.NewProc("sqlite3_finalize")
var sqliteBind = sqliteDLL.NewProc("sqlite3_bind_text")
var sqliteParamCount = sqliteDLL.NewProc("sqlite3_bind_parameter_count")
var sqliteStep = sqliteDLL.NewProc("sqlite3_step")
var sqliteColumnCount = sqliteDLL.NewProc("sqlite3_column_count")
var sqliteColumnText = sqliteDLL.NewProc("sqlite3_column_text")
var sqliteColumnBytes = sqliteDLL.NewProc("sqlite3_column_bytes")
var sqliteBusy = sqliteDLL.NewProc("sqlite3_busy_timeout")

func nativeOpen(path string) (uintptr, error) {
	if e := sqliteDLL.Load(); e != nil {
		return 0, errors.New("sqlite: Windows system SQLite unavailable")
	}
	p, e := syscall.BytePtrFromString(path)
	if e != nil {
		return 0, e
	}
	var h uintptr
	r, _, _ := sqliteOpen.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&h)), 0x10006, 0)
	runtime.KeepAlive(p)
	if r != 0 {
		if h != 0 {
			sqliteClose.Call(h)
		}
		return 0, errors.New("sqlite: cannot open database")
	}
	sqliteBusy.Call(h, 5000)
	return h, nil
}
func nativeClose(h uintptr) error {
	r, _, _ := sqliteClose.Call(h)
	if r != 0 {
		return errors.New("sqlite: close failed")
	}
	return nil
}
func nativeQuery(h uintptr, q string, args []string) ([][]string, error) {
	p, e := syscall.BytePtrFromString(q)
	if e != nil {
		return nil, e
	}
	var s uintptr
	r, _, _ := sqlitePrepare.Call(h, uintptr(unsafe.Pointer(p)), ^uintptr(0), uintptr(unsafe.Pointer(&s)), 0)
	runtime.KeepAlive(p)
	if r != 0 || s == 0 {
		return nil, errors.New("sqlite: prepare failed")
	}
	defer sqliteFinalize.Call(s)
	count, _, _ := sqliteParamCount.Call(s)
	if int(count) != len(args) {
		return nil, errors.New("sqlite: parameter count mismatch")
	}
	for i, a := range args {
		buf := append([]byte(a), 0)
		r, _, _ = sqliteBind.Call(s, uintptr(i+1), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(a)), ^uintptr(0))
		runtime.KeepAlive(buf)
		if r != 0 {
			return nil, errors.New("sqlite: bind failed")
		}
	}
	rows := make([][]string, 0)
	for {
		r, _, _ = sqliteStep.Call(s)
		if r == 101 {
			return rows, nil
		}
		if r != 100 {
			return nil, errors.New("sqlite: step failed")
		}
		n, _, _ := sqliteColumnCount.Call(s)
		row := make([]string, n)
		for i := range row {
			ptr, _, _ := sqliteColumnText.Call(s, uintptr(i))
			size, _, _ := sqliteColumnBytes.Call(s, uintptr(i))
			if size > 16<<20 {
				return nil, errors.New("sqlite: column exceeds limit")
			}
			if ptr != 0 && size > 0 {
				// Copy foreign SQLite memory without fabricating a Go pointer from an integer.
				buf := make([]byte, int(size))
				copyNativeMemory.Call(uintptr(unsafe.Pointer(&buf[0])), ptr, size)
				row[i] = string(buf)
				runtime.KeepAlive(buf)
			}
		}
		rows = append(rows, row)
	}
}
