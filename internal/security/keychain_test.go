package security

import (
	"errors"
	"path/filepath"
	"starling/internal/model"
	"testing"
)

type memoryKeychain struct {
	data    []byte
	failure error
}

func (k *memoryKeychain) Available() bool { return true }
func (k *memoryKeychain) Read(string) ([]byte, error) {
	if k.failure != nil {
		return nil, k.failure
	}
	if k.data == nil {
		return nil, errKeychainMissing
	}
	return append([]byte(nil), k.data...), nil
}
func (k *memoryKeychain) Write(_ string, b []byte) error {
	if k.failure != nil {
		return k.failure
	}
	k.data = append([]byte(nil), b...)
	return nil
}
func (k *memoryKeychain) Delete(string) error {
	if k.failure != nil {
		return k.failure
	}
	k.data = nil
	return nil
}

func TestKeychainRotationClearAndIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.vault")
	k := &memoryKeychain{}
	v := newKeychainVault(path, k)
	if v.account == newKeychainVault(path+"-other", k).account {
		t.Fatal("profiles share a keychain item")
	}
	if v.account != newKeychainVault(path, k).account {
		t.Fatal("unstable account")
	}
	if s, e := v.Load(); e != nil || s.Credentials.Access != "" {
		t.Fatal("missing item must be guest", e)
	}
	s := model.SavedSession{Credentials: model.Credentials{Access: "synthetic-one", Refresh: "synthetic-refresh"}, Identity: model.Identity{ID: "synthetic"}}
	for _, token := range []string{"synthetic-one", "synthetic-two"} {
		s.Credentials.Access = token
		if e := v.Save(s); e != nil {
			t.Fatal(e)
		}
		got, e := v.Load()
		if e != nil || got.Credentials.Access != token {
			t.Fatal("rotation", e)
		}
	}
	if e := v.Clear(); e != nil {
		t.Fatal(e)
	}
	if got, e := v.Load(); e != nil || got.Credentials.Access != "" {
		t.Fatal("clear", e)
	}
	k.failure = errors.New("synthetic native failure")
	if v.Save(s) == nil {
		t.Fatal("save failure hidden")
	}
	if _, e := v.Load(); e == nil {
		t.Fatal("read failure hidden")
	}
	if v.Clear() == nil {
		t.Fatal("delete failure hidden")
	}
}

func TestKeychainRejectsCorruptOrOversizedData(t *testing.T) {
	k := &memoryKeychain{}
	v := newKeychainVault("synthetic", k)
	for _, b := range [][]byte{[]byte("broken"), []byte(`{}`), make([]byte, 65537)} {
		k.data = b
		if _, e := v.Load(); e == nil {
			t.Fatal("accepted corrupt saved session")
		}
	}
}
