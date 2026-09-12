package demoweb

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDemoAPICannotBeCalledWithoutCapability(t *testing.T) {
	s, e := New(t.TempDir(), "127.0.0.1:34115")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:34115/api", strings.NewReader(`{"action":"bootstrap","payload":{}}`))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestDemoRejectsCrossOriginScript(t *testing.T) {
	s, e := New(t.TempDir(), "127.0.0.1:34115")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r := httptest.NewRequest("GET", "http://127.0.0.1:34115/demo-config.js", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
