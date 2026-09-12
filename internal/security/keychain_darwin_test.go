//go:build darwin && cgo

package security

import (
	"os/exec"
	"path/filepath"
	"starling/internal/model"
	"testing"
)

func TestNativeKeychainRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.keychain-db")
	run := func(args ...string) {
		t.Helper()
		if b, e := exec.Command("/usr/bin/security", args...).CombinedOutput(); e != nil {
			t.Fatalf("security failed: %v: %s", e, b)
		}
	}
	run("create-keychain", "-p", "starling-synthetic-test-only", path)
	t.Cleanup(func() { _ = exec.Command("/usr/bin/security", "delete-keychain", path).Run() })
	run("unlock-keychain", "-p", "starling-synthetic-test-only", path)
	backend, release, e := openIsolatedKeychain(path)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	v := newKeychainVault(filepath.Join(t.TempDir(), "session.vault"), backend)
	saved := model.SavedSession{Credentials: model.Credentials{Access: "synthetic-access", Refresh: "synthetic-refresh"}, Identity: model.Identity{ID: "synthetic-user"}}
	if e = v.Save(saved); e != nil {
		t.Fatal(e)
	}
	saved.Credentials.Access = "synthetic-rotated"
	if e = v.Save(saved); e != nil {
		t.Fatal(e)
	}
	got, e := v.Load()
	if e != nil || got != saved {
		t.Fatal("round trip failed", e)
	}
	other := newKeychainVault(filepath.Join(t.TempDir(), "session.vault"), backend)
	if got, e := other.Load(); e != nil || got.Credentials.Access != "" {
		t.Fatal("profile isolation failed", e)
	}
	if e = v.Clear(); e != nil {
		t.Fatal(e)
	}
	if got, e := v.Load(); e != nil || got.Credentials.Access != "" {
		t.Fatal("clear failed", e)
	}
	if e = v.Clear(); e != nil {
		t.Fatal("idempotent clear", e)
	}
}
