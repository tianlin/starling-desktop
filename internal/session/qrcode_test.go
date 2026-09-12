package session

import (
	"context"
	"starling/internal/model"
	"starling/internal/provider"
	"starling/internal/testkit"
	"testing"
)

type qrFake struct {
	testkit.Fake
	status string
}

func (f *qrFake) CreateQR(context.Context) (provider.QRCode, error) {
	return provider.QRCode{ID: "test", URL: "https://example.com"}, nil
}
func (f *qrFake) PollQR(context.Context, string) (string, model.Credentials, error) {
	return f.status, model.Credentials{Access: "access", Refresh: "refresh"}, nil
}
func (f *qrFake) QRIdentity(context.Context, string) (model.Identity, error) {
	return model.Identity{ID: "user-a"}, nil
}

func TestQRCancelPreventsLateLogin(t *testing.T) {
	f := &qrFake{status: "CONFIRMED"}
	m := New(f, &testkit.MemoryVault{})
	defer m.Close()
	q, e := m.StartQR(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	m.CancelQR(q.ID)
	if _, e = m.PollQR(context.Background(), q.ID, false); e == nil {
		t.Fatal("cancelled QR was accepted")
	}
	if m.View().State != "guest" {
		t.Fatal("cancel did not restore guest")
	}
}

func TestQREmptyCancelDoesNotCancelAnotherAttempt(t *testing.T) {
	f := &qrFake{status: "SCANNED"}
	m := New(f, &testkit.MemoryVault{})
	defer m.Close()
	q, e := m.StartQR(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	m.CancelQR("")
	if _, e = m.PollQR(context.Background(), q.ID, false); e != nil {
		t.Fatal(e)
	}
}

func TestOldQRDoesNotOwnNewLogin(t *testing.T) {
	f := &qrFake{status: "SCANNED"}
	m := New(f, &testkit.MemoryVault{})
	defer m.Close()
	q, e := m.StartQR(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	_ = m.finishFailure(m.View().Epoch, model.Err("QR_EXPIRED", "expired"))
	next, e := m.begin()
	if e != nil {
		t.Fatal(e)
	}
	m.CancelQR(q.ID)
	if m.View().Epoch != next.Epoch || m.View().State != "connecting" {
		t.Fatal("old QR cancelled new login")
	}
	if _, e = m.PollQR(context.Background(), q.ID, false); e == nil {
		t.Fatal("old QR polled during new login")
	}
}
func TestQRRequiresMatchingListenerIdentity(t *testing.T) {
	f := &qrFake{status: "CONFIRMED"}
	f.MeFunc = func(context.Context, string) (model.Identity, error) { return model.Identity{ID: "other"}, nil }
	m := New(f, &testkit.MemoryVault{})
	defer m.Close()
	q, e := m.StartQR(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	_, e = m.PollQR(context.Background(), q.ID, false)
	if !model.IsCode(e, "IDENTITY_MISMATCH") || m.View().State != "guest" {
		t.Fatalf("%v %v", m.View(), e)
	}
}
func TestQRConnectsOnlyAfterConfirmation(t *testing.T) {
	f := &qrFake{status: "SCANNED"}
	m := New(f, &testkit.MemoryVault{})
	defer m.Close()
	q, e := m.StartQR(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.PollQR(context.Background(), q.ID, false); e != nil {
		t.Fatal(e)
	}
	if m.View().Identity != nil {
		t.Fatal("connected before confirmation")
	}
	f.status = "CONFIRMED"
	if _, e = m.PollQR(context.Background(), q.ID, false); e != nil {
		t.Fatal(e)
	}
	if m.View().State != "connected" {
		t.Fatal(m.View())
	}
}
