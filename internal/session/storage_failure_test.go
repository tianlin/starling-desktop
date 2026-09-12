package session

import (
	"context"
	"starling/internal/model"
	"starling/internal/testkit"
	"testing"
)

func TestSaveFailureKeepsExplicitTemporarySession(t *testing.T) {
	v := &testkit.MemoryVault{SaveErr: model.Err("SECURE_STORAGE", "synthetic storage denial")}
	m := New(&testkit.Fake{}, v)
	defer m.Close()
	if e := m.Login(context.Background(), "00000000000", "+86", "0000", true); e != nil {
		t.Fatal(e)
	}
	view := m.View()
	if view.State != "connected" || view.Persistent || view.StorageWarning == "" {
		t.Fatal("save failure must be visible and session temporary", view)
	}
	v.ClearErr = model.Err("SECURE_STORAGE", "synthetic delete failure")
	if e := m.Logout(nil); e == nil {
		t.Fatal("logout hid keychain removal failure")
	}
}

func TestRefreshSaveFailureUsesRotatedCredentialsTemporarily(t *testing.T) {
	m, v := loggedIn(t, &testkit.Fake{})
	v.SaveErr = model.Err("SECURE_STORAGE", "synthetic storage denial")
	_, e := m.Do(context.Background(), func(_ context.Context, token string) error {
		if token == "old" {
			return model.Err("UNAUTHORIZED", "synthetic expired token")
		}
		return nil
	})
	if e != nil {
		t.Fatal("rotation should remain usable in memory", e)
	}
	if view := m.View(); view.State != "connected" || view.Persistent || view.StorageWarning == "" {
		t.Fatal("lost temporary rotation", view)
	}
}
