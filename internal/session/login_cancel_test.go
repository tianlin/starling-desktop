package session

import (
	"context"
	"starling/internal/model"
	"starling/internal/testkit"
	"testing"
)

func TestCancelQueuedLogin(t *testing.T) {
	f := &testkit.Fake{LoginFunc: func(context.Context, string, string, string) (model.Credentials, model.Identity, error) {
		t.Error("cancelled request reached provider")
		return model.Credentials{}, model.Identity{}, nil
	}}
	m := New(f, &testkit.MemoryVault{})
	defer m.Close()
	epoch := m.View().Epoch
	if e := m.CancelLogin(epoch); e != nil {
		t.Fatal(e)
	}
	if e := m.LoginAt(context.Background(), epoch, "00000000000", "+86", "1234", true); !model.IsCode(e, "STALE_SESSION") {
		t.Fatalf("expected stale: %v", e)
	}
}

func TestCancelLoginDiscardsLateCredentials(t *testing.T) {
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	f := &testkit.Fake{LoginFunc: func(ctx context.Context, _, _, _ string) (model.Credentials, model.Identity, error) {
		started <- ctx
		<-release
		return model.Credentials{Access: "late", Refresh: "late-refresh"}, model.Identity{ID: "user-a"}, nil
	}}
	v := &testkit.MemoryVault{}
	m := New(f, v)
	defer m.Close()
	epoch := m.View().Epoch
	result := make(chan error, 1)
	go func() { result <- m.LoginAt(context.Background(), epoch, "00000000000", "+86", "1234", true) }()
	ctx := <-started
	if e := m.CancelLogin(epoch); e != nil {
		t.Fatal(e)
	}
	if ctx.Err() == nil {
		t.Error("provider context still active")
	}
	close(release)
	if e := <-result; !model.IsCode(e, "STALE_SESSION") {
		t.Fatalf("late login: %v", e)
	}
	if m.View().Identity != nil || v.Saves != 0 {
		t.Fatal("cancelled login persisted")
	}
}

func TestCancelLoginCannotCancelOtherAttempts(t *testing.T) {
	m := New(&testkit.Fake{}, &testkit.MemoryVault{})
	defer m.Close()
	old := m.View().Epoch
	if _, e := m.begin(); e != nil {
		t.Fatal(e)
	} // QR and restore share the non-SMS begin path.
	if e := m.CancelLogin(old); !model.IsCode(e, "STALE_SESSION") {
		t.Fatalf("cancelled non-SMS attempt: %v", e)
	}
	if m.View().State != "connecting" {
		t.Fatal("other attempt changed")
	}
}

func TestCancelCompletedLoginKeepsAccount(t *testing.T) {
	m := New(&testkit.Fake{}, &testkit.MemoryVault{})
	defer m.Close()
	epoch := m.View().Epoch
	if e := m.LoginAt(context.Background(), epoch, "00000000000", "+86", "1234", true); e != nil {
		t.Fatal(e)
	}
	if e := m.CancelLogin(epoch); !model.IsCode(e, "LOGIN_COMPLETED") {
		t.Fatalf("completed cancellation: %v", e)
	}
	if m.View().Identity == nil {
		t.Fatal("cancellation logged out account")
	}
	if e := m.Logout(nil); e != nil {
		t.Fatal(e)
	}
	if e := m.Login(context.Background(), "00000000000", "+86", "1234", true); e != nil {
		t.Fatal(e)
	}
	if e := m.CancelLogin(epoch); !model.IsCode(e, "STALE_SESSION") {
		t.Fatalf("old cancellation affected new login: %v", e)
	}
	if m.View().Identity == nil {
		t.Fatal("new account lost")
	}
}
