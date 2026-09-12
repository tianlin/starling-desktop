package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"starling/internal/model"
	"testing"
)

func TestQRLiveWaiting(t *testing.T) {
	if os.Getenv("STARLING_QR_LIVE") != "1" {
		t.Skip("explicit live probe only")
	}
	c := New()
	q, e := c.CreateQR(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	status, _, e := c.PollQR(context.Background(), q.ID)
	if e != nil || status != "WAITTING" {
		t.Fatalf("status=%s error=%v", status, e)
	}
}

func TestQRLoginStatusesAndCookies(t *testing.T) {
	status := "WAITTING"
	withCredentials := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/qrcode/create" {
			if r.Header.Get("x-midway-app-id") == "" {
				t.Error("missing web application header")
			}
			w.Write([]byte(`{"id":"6aa4b6d47c72d6f27493ca4a","url":"https://h5.xiaoyuzhoufm.com/oauth?qrcode_id=6aa4b6d47c72d6f27493ca4a"}`))
			return
		}
		if status == "EXPIRED" {
			w.WriteHeader(401)
			w.Write([]byte(`{"code":21}`))
			return
		}
		if withCredentials && (status == "CONFIRMED" || status == "USED") {
			http.SetCookie(w, &http.Cookie{Name: "x-jike-access-token", Value: "access"})
			http.SetCookie(w, &http.Cookie{Name: "x-jike-refresh-token", Value: "refresh"})
		}
		w.Write([]byte(`{"status":"` + status + `"}`))
	}))
	defer server.Close()
	c := newClient(server.Client(), server.URL, server.URL, server.URL)
	c.qrOrigin = server.URL
	q, e := c.CreateQR(context.Background())
	if e != nil || q.ID == "" {
		t.Fatalf("create: %v", e)
	}
	for _, s := range []string{"WAITTING", "SCANNED", "CONFIRMED", "USED"} {
		status = s
		got, creds, e := c.PollQR(context.Background(), q.ID)
		expected := s
		if s == "USED" {
			expected = "CONFIRMED"
		}
		if e != nil || got != expected {
			t.Fatalf("%s: %s %v", s, got, e)
		}
		if (s == "CONFIRMED" || s == "USED") && (creds.Access != "access" || creds.Refresh != "refresh") {
			t.Fatal("credentials lost")
		}
		if s != "CONFIRMED" && s != "USED" && creds.Access != "" {
			t.Fatal("premature credentials")
		}
	}
	status = "USED"
	withCredentials = false
	_, _, e = c.PollQR(context.Background(), q.ID)
	if !model.IsCode(e, "BAD_RESPONSE") {
		t.Fatalf("USED without credentials must not connect: %v", e)
	}
	status = "EXPIRED"
	_, _, e = c.PollQR(context.Background(), q.ID)
	if !model.IsCode(e, "QR_EXPIRED") {
		t.Fatalf("expiry: %v", e)
	}
}
