package session

import (
	"context"
	"starling/internal/model"
	"starling/internal/testkit"
	"testing"
)

func TestDoOnceAtNeverRefreshesOrReplays(t *testing.T) {
	refresh, attempts := 0, 0
	f := &testkit.Fake{RefreshFunc: func(context.Context, model.Credentials) (model.Credentials, error) {
		refresh++
		return model.Credentials{}, nil
	}}
	m, _ := loggedIn(t, f)
	_, err := m.DoOnceAt(context.Background(), m.View().Epoch, func(context.Context, string) error { attempts++; return model.Err("UNAUTHORIZED", "expired") })
	if !model.IsCode(err, "UNAUTHORIZED") || attempts != 1 || refresh != 0 || m.View().State != "needs_login" {
		t.Fatal(err, attempts, refresh, m.View())
	}
}

func TestDoOnceAtRejectsStaleAndCancelledBeforeCallback(t *testing.T) {
	m, _ := loggedIn(t, &testkit.Fake{})
	f := func(context.Context, string) error { t.Fatal("invalid write sent"); return nil }
	if _, err := m.DoOnceAt(context.Background(), m.View().Epoch-1, f); !model.IsCode(err, "STALE_SESSION") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.DoOnceAt(ctx, m.View().Epoch, f); !model.IsCode(err, "CANCELLED") {
		t.Fatal(err)
	}
}

func TestDoOnceAtDiscardsSuccessAfterLogout(t *testing.T) {
	m, _ := loggedIn(t, &testkit.Fake{})
	epoch := m.View().Epoch
	_, err := m.DoOnceAt(context.Background(), epoch, func(context.Context, string) error {
		if e := m.Logout(nil); e != nil {
			t.Fatal(e)
		}
		return nil
	})
	if !model.IsCode(err, "STALE_SESSION") {
		t.Fatal(err)
	}
}
