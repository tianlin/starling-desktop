//go:build windows

package security

import (
	"bytes"
	"testing"
)

func TestNativeUserDPAPIRoundTrip(t *testing.T) {
	p := nativeProtector{}
	plain := []byte("synthetic credential, not an account token")
	sealed, e := p.Protect(plain)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(sealed, plain) {
		t.Fatal("plaintext stored")
	}
	opened, e := p.Unprotect(sealed)
	if e != nil || !bytes.Equal(opened, plain) {
		t.Fatal(e)
	}
	sealed[len(sealed)/2] ^= 1
	if _, e = p.Unprotect(sealed); e == nil {
		t.Fatal("tampering accepted")
	}
}
