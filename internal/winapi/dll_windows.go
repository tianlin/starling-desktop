//go:build windows

// Package winapi resolves system DLLs by an absolute trusted system-directory path.
package winapi

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

func SystemDLL(name string) *syscall.LazyDLL {
	if filepath.Base(name) != name {
		panic("invalid system DLL name")
	}
	kernel := syscall.NewLazyDLL("kernel32.dll") // Windows KnownDLL, needed to locate System32 itself.
	proc := kernel.NewProc("GetSystemDirectoryW")
	buf := make([]uint16, 32768)
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || n >= uintptr(len(buf)) {
		panic("Windows system directory unavailable")
	}
	return syscall.NewLazyDLL(filepath.Join(syscall.UTF16ToString(buf[:n]), name))
}
