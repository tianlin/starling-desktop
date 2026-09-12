//go:build windows && amd64

package desktop

import (
	"testing"
	"unsafe"
)

func TestWindowsABI(t *testing.T) {
	if unsafe.Sizeof(notifyData{}) != 976 {
		t.Fatalf("NOTIFYICONDATAW size=%d", unsafe.Sizeof(notifyData{}))
	}
	if unsafe.Sizeof(windowClass{}) != 80 {
		t.Fatalf("WNDCLASSEXW size=%d", unsafe.Sizeof(windowClass{}))
	}
	if unsafe.Sizeof(message{}) != 48 {
		t.Fatalf("MSG size=%d", unsafe.Sizeof(message{}))
	}
}
