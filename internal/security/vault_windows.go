//go:build windows

package security

import (
	"runtime"
	"starling/internal/winapi"
	"syscall"
	"unsafe"
)

var crypt32 = winapi.SystemDLL("crypt32.dll")
var cryptProtect = crypt32.NewProc("CryptProtectData")
var cryptUnprotect = crypt32.NewProc("CryptUnprotectData")
var kernel32 = winapi.SystemDLL("kernel32.dll")
var localFree = kernel32.NewProc("LocalFree")
var moveFileEx = kernel32.NewProc("MoveFileExW")

type dataBlob struct {
	Size uint32
	Data *byte
}
type nativeProtector struct{}

func (nativeProtector) Available() bool {
	return cryptProtect.Find() == nil && cryptUnprotect.Find() == nil
}
func (nativeProtector) Protect(b []byte) ([]byte, error)   { return crypt(b, true) }
func (nativeProtector) Unprotect(b []byte) ([]byte, error) { return crypt(b, false) }
func crypt(b []byte, encrypt bool) ([]byte, error) {
	if len(b) == 0 {
		return nil, errNativeProtection
	}
	in := dataBlob{Size: uint32(len(b)), Data: &b[0]}
	var out dataBlob
	var ok uintptr
	// CRYPTPROTECT_UI_FORBIDDEN only: never use CRYPTPROTECT_LOCAL_MACHINE.
	if encrypt {
		ok, _, _ = cryptProtect.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	} else {
		ok, _, _ = cryptUnprotect.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	}
	runtime.KeepAlive(b)
	if ok == 0 {
		return nil, errNativeProtection
	}
	defer localFree.Call(uintptr(unsafe.Pointer(out.Data)))
	if out.Size > 65536 {
		return nil, errNativeProtection
	}
	return append([]byte{}, unsafe.Slice(out.Data, int(out.Size))...), nil
}
func replaceFile(a, b string) error {
	pa, e := syscall.UTF16PtrFromString(a)
	if e != nil {
		return e
	}
	pb, e := syscall.UTF16PtrFromString(b)
	if e != nil {
		return e
	}
	ok, _, er := moveFileEx.Call(uintptr(unsafe.Pointer(pa)), uintptr(unsafe.Pointer(pb)), 0x1|0x8)
	runtime.KeepAlive(pa)
	runtime.KeepAlive(pb)
	if ok == 0 {
		return er
	}
	return nil
}
