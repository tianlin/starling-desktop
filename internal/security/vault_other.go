//go:build !windows

package security

import "os"

type nativeProtector struct{}

func (nativeProtector) Available() bool                  { return false }
func (nativeProtector) Protect([]byte) ([]byte, error)   { return nil, errNativeProtection }
func (nativeProtector) Unprotect([]byte) ([]byte, error) { return nil, errNativeProtection }
func replaceFile(a, b string) error {
	if e := os.Rename(a, b); e != nil {
		return e
	}
	return nil
}
