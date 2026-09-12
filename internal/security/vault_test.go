package security

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"starling/internal/model"
	"testing"
)

type testProtector struct{}

func (testProtector) Protect(b []byte) ([]byte, error) {
	out := append([]byte{}, b...)
	for i := range out {
		out[i] ^= 0xA5
	}
	return out, nil
}
func (testProtector) Unprotect(b []byte) ([]byte, error) { return testProtector{}.Protect(b) }
func (testProtector) Available() bool                    { return true }

type brokenProtector struct{ testProtector }

func (brokenProtector) Protect([]byte) ([]byte, error) { return nil, errors.New("unavailable") }
func TestVaultRoundTripRotationClear(t *testing.T) {
	p := filepath.Join(t.TempDir(), "session.vault")
	v := newVault(p, testProtector{})
	saved := model.SavedSession{Credentials: model.Credentials{Access: "token-one", Refresh: "refresh-one"}, Identity: model.Identity{ID: "u", Nickname: "name"}}
	if e := v.Save(saved); e != nil {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(p)
	if bytes.Contains(raw, []byte("token-one")) {
		t.Fatal("plaintext")
	}
	saved.Credentials.Access = "token-two"
	if e := v.Save(saved); e != nil {
		t.Fatal(e)
	}
	got, e := v.Load()
	if e != nil || got.Credentials.Access != "token-two" {
		t.Fatal(got, e)
	}
	if e = v.Clear(); e != nil {
		t.Fatal(e)
	}
	got, e = v.Load()
	if e != nil || got.Credentials.Access != "" {
		t.Fatal(got, e)
	}
}
func TestVaultNeverFallsBackToPlaintext(t *testing.T) {
	p := filepath.Join(t.TempDir(), "session.vault")
	v := newVault(p, brokenProtector{})
	e := v.Save(model.SavedSession{Credentials: model.Credentials{Access: "secret"}})
	if e == nil {
		t.Fatal("expected protect failure")
	}
	if _, e = os.Stat(p); !os.IsNotExist(e) {
		t.Fatal("wrote unprotected file")
	}
}
