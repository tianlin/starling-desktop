package session

import (
	"context"
	"encoding/json"
	"starling/internal/model"
	"starling/internal/testkit"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func loggedIn(t *testing.T, f *testkit.Fake) (*Manager, *testkit.MemoryVault) {
	t.Helper()
	v := &testkit.MemoryVault{}
	m := New(f, v)
	if e := m.Login(context.Background(), "00000000000", "+86", "123456", true); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	return m, v
}
func TestConcurrentUnauthorizedSingleRefresh(t *testing.T) {
	f := &testkit.Fake{}
	var refresh atomic.Int32
	f.RefreshFunc = func(c context.Context, old model.Credentials) (model.Credentials, error) {
		refresh.Add(1)
		time.Sleep(20 * time.Millisecond)
		return model.Credentials{Access: "new", Refresh: "rotated"}, nil
	}
	m, v := loggedIn(t, f)
	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := m.Do(context.Background(), func(ctx context.Context, token string) error {
				if token == "old" {
					return model.Err("UNAUTHORIZED", "expired")
				}
				if token != "new" {
					t.Error("wrong token")
				}
				return nil
			})
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Error(e)
		}
	}
	if refresh.Load() != 1 {
		t.Fatalf("refresh calls=%d", refresh.Load())
	}
	saved, _ := v.Load()
	if saved.Credentials.Refresh != "rotated" {
		t.Fatal("rotation not saved")
	}
}
func TestTransientRefreshKeepsCredentials(t *testing.T) {
	f := &testkit.Fake{RefreshFunc: func(context.Context, model.Credentials) (model.Credentials, error) {
		return model.Credentials{}, model.Err("NETWORK", "offline")
	}}
	m, v := loggedIn(t, f)
	_, e := m.Do(context.Background(), func(context.Context, string) error { return model.Err("UNAUTHORIZED", "expired") })
	if !model.IsCode(e, "NETWORK") {
		t.Fatal(e)
	}
	saved, _ := v.Load()
	if saved.Credentials.Access != "old" || m.View().State != "connected" {
		t.Fatal("erased on network failure")
	}
}
func TestUnauthorizedReplayOnlyOnce(t *testing.T) {
	m, _ := loggedIn(t, &testkit.Fake{})
	var calls int
	_, e := m.Do(context.Background(), func(context.Context, string) error { calls++; return model.Err("UNAUTHORIZED", "expired") })
	if !model.IsCode(e, "UNAUTHORIZED") || calls != 2 || m.View().State != "needs_login" {
		t.Fatal(e, calls, m.View())
	}
}
func TestLogoutRejectsLateResponse(t *testing.T) {
	m, _ := loggedIn(t, &testkit.Fake{})
	start := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, e := m.Do(context.Background(), func(context.Context, string) error { close(start); <-release; return nil })
		result <- e
	}()
	<-start
	old := m.Snapshot()
	if e := m.Logout(nil); e != nil {
		t.Fatal(e)
	}
	close(release)
	if e := <-result; !model.IsCode(e, "STALE_SESSION") {
		t.Fatal(e)
	}
	written := false
	e := m.Commit(old.Epoch, func(string) error { written = true; return nil })
	if e == nil || written {
		t.Fatal("stale commit accepted")
	}
}
func TestLoginIdentityMismatchRejected(t *testing.T) {
	f := &testkit.Fake{MeFunc: func(context.Context, string) (model.Identity, error) { return model.Identity{ID: "different"}, nil }}
	m := New(f, &testkit.MemoryVault{})
	defer m.Close()
	if e := m.Login(context.Background(), "00000000000", "+86", "123456", false); !model.IsCode(e, "IDENTITY_MISMATCH") {
		t.Fatal(e)
	}
	if m.View().State == "connected" {
		t.Fatal("mismatch connected")
	}
}
func TestNoTokenInPublicSession(t *testing.T) {
	m, _ := loggedIn(t, &testkit.Fake{})
	b, _ := json.Marshal(m.View())
	if strings.Contains(string(b), "refresh") || strings.Contains(string(b), "old") {
		t.Fatal(string(b))
	}
}
func TestLogoutDuringLogin(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	f := &testkit.Fake{LoginFunc: func(context.Context, string, string, string) (model.Credentials, model.Identity, error) {
		close(started)
		<-release
		return model.Credentials{Access: "late", Refresh: "late-refresh"}, model.Identity{ID: "user-a"}, nil
	}}
	v := &testkit.MemoryVault{}
	m := New(f, v)
	defer m.Close()
	result := make(chan error, 1)
	go func() { result <- m.Login(context.Background(), "00000000000", "+86", "123456", true) }()
	<-started
	m.Logout(nil)
	close(release)
	if e := <-result; !model.IsCode(e, "STALE_SESSION") {
		t.Fatal(e)
	}
	saved, _ := v.Load()
	if saved.Credentials.Access != "" {
		t.Fatal("late credentials persisted")
	}
}

func TestStaleLogoutCannotClearNewAccount(t *testing.T) {
	p := &testkit.Fake{}
	v := &testkit.MemoryVault{}
	m := New(p, v)
	defer m.Close()
	old := m.View().Epoch
	if e := m.Login(context.Background(), "00000000000", "+86", "123456", false); e != nil {
		t.Fatal(e)
	}
	called := false
	if e := m.LogoutAt(old, func(string) error { called = true; return nil }); !model.IsCode(e, "STALE_SESSION") {
		t.Fatal(e)
	}
	if called || m.View().State != "connected" {
		t.Fatal("stale logout changed session")
	}
}

func TestReplaySnapshotRejectsChangedEpoch(t *testing.T) {
	m := New(&testkit.Fake{}, &testkit.MemoryVault{})
	defer m.Close()
	if e := m.Login(context.Background(), "00000000000", "+86", "123456", false); e != nil {
		t.Fatal(e)
	}
	old := m.View().Epoch
	m.Logout(nil)
	if e := m.Login(context.Background(), "00000000000", "+86", "123456", false); e != nil {
		t.Fatal(e)
	}
	if _, e := m.connectedSnapshot(old); !model.IsCode(e, "STALE_SESSION") {
		t.Fatal(e)
	}
	called := false
	if _, e := m.DoAt(context.Background(), old, func(context.Context, string) error {
		called = true
		return nil
	}); !model.IsCode(e, "STALE_SESSION") || called {
		t.Fatalf("old epoch callback called=%v, err=%v", called, e)
	}
}

type delayedLoadVault struct {
	*testkit.MemoryVault
	started, release chan struct{}
}

func (v *delayedLoadVault) Load() (model.SavedSession, error) {
	saved, e := v.MemoryVault.Load()
	close(v.started)
	<-v.release
	return saved, e
}
func TestLogoutWhileLoadingSavedVaultCannotRestoreIt(t *testing.T) {
	v := &delayedLoadVault{MemoryVault: &testkit.MemoryVault{Value: model.SavedSession{Credentials: model.Credentials{Access: "old", Refresh: "old-refresh"}, Identity: model.Identity{ID: "user-a"}}}, started: make(chan struct{}), release: make(chan struct{})}
	m := New(&testkit.Fake{}, v)
	defer m.Close()
	done := make(chan error, 1)
	go func() { done <- m.Restore(context.Background()) }()
	<-v.started
	if e := m.Logout(nil); e != nil {
		t.Fatal(e)
	}
	close(v.release)
	if e := <-done; !model.IsCode(e, "STALE_SESSION") {
		t.Fatal("restore after logout", e)
	}
	if m.View().Identity != nil {
		t.Fatal("restored logged-out account")
	}
}
