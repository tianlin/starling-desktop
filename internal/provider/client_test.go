package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"starling/internal/model"
	"strings"
	"sync/atomic"
	"testing"
)

func fixtureClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return newClient(s.Client(), s.URL, s.URL, s.URL)
}
func TestSMSAndIdentity(t *testing.T) {
	var codeCalls atomic.Int32
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/send-code":
			codeCalls.Add(1)
			fmt.Fprint(w, `{"data":{}}`)
		case "/v1/auth/login-with-sms":
			var in map[string]string
			json.NewDecoder(r.Body).Decode(&in)
			if in["verifyCode"] != "123456" {
				t.Error("wrong body")
			}
			w.Header().Set("x-jike-access-token", "access-fixture")
			w.Header().Set("x-jike-refresh-token", "refresh-fixture")
			fmt.Fprint(w, `{"data":{"user":{"uid":"user-a","nickname":"测试账号"}}}`)
		case "/v1/profile/get":
			if r.Header.Get("x-jike-access-token") != "access-fixture" {
				t.Error("no auth")
			}
			fmt.Fprint(w, `{"data":{"user":{"uid":"user-a","nickname":"测试账号"}}}`)
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	})
	ctx := context.Background()
	if e := c.SendCode(ctx, "00000000000", "+86"); e != nil {
		t.Fatal(e)
	}
	creds, id, e := c.Login(ctx, "00000000000", "+86", "123456")
	if e != nil || creds.Access != "access-fixture" || id.ID != "user-a" {
		t.Fatal(creds, id, e)
	}
	me, e := c.Me(ctx, creds.Access)
	if e != nil || me.ID != id.ID {
		t.Fatal(me, e)
	}
	if codeCalls.Load() != 1 {
		t.Fatal("duplicate SMS")
	}
}
func TestListContractAndReadOnly(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/favorite/list" || r.Method != "POST" {
			t.Fatal(r.URL, r.Method)
		}
		fmt.Fprint(w, `{"data":[{"eid":"64db2d493fa4090b744c313c","title":"收藏单集","duration":100,"podcast":{"pid":"64db2d493fa4090b744c313d","title":"节目"},"enclosure":{"url":"https://media.xyzcdn.net/audio.m4a"}}],"loadMoreKey":null}`)
	})
	p, e := c.List(context.Background(), "token", "favorites", "", "")
	if e != nil || !p.Complete || len(p.Items) != 1 || p.Items[0].PodcastTitle != "节目" {
		t.Fatal(p, e)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "audio.m4a") {
		t.Fatal("persisted media URL")
	}
}
func TestRateLimitIsNotRetried(t *testing.T) {
	var n atomic.Int32
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.Header().Set("Retry-After", "17")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"secret":"never disclose"}`)
	})
	_, e := c.Me(context.Background(), "secret-token")
	if !model.IsCode(e, "RATE_LIMIT") || model.PublicError(e).RetryAfter < 16 || n.Load() != 1 {
		t.Fatal(e, n.Load())
	}
	if strings.Contains(e.Error(), "secret") {
		t.Fatal("leaked upstream")
	}
}
func TestAuthNotRetried(t *testing.T) {
	var n atomic.Int32
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { n.Add(1); w.WriteHeader(401) })
	_, _, e := c.Login(context.Background(), "00000000000", "+86", "123456")
	if !model.IsCode(e, "UNAUTHORIZED") || n.Load() != 1 {
		t.Fatal(e, n.Load())
	}
}
func TestRedirectDoesNotLeakToken(t *testing.T) {
	var received atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1) }))
	defer other.Close()
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 302) })
	_, e := c.Me(context.Background(), "private-token")
	if e == nil || received.Load() != 0 {
		t.Fatal(e, received.Load())
	}
}
func TestMalformedSuccessIsNotEmptyLibrary(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"unexpected":[]}`) })
	_, e := c.List(context.Background(), "token", "favorites", "", "")
	if !model.IsCode(e, "BAD_RESPONSE") {
		t.Fatal(e)
	}
}
func TestPaidContentNotResolved(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":{"eid":"64db2d493fa4090b744c313c","title":"付费","payType":"PAY_EPISODE","enclosure":{"url":"https://media.xyzcdn.net/paid.mp3"}}}`)
	})
	it, e := c.Detail(context.Background(), "token", "episode", "64db2d493fa4090b744c313c")
	if e != nil || !it.Restricted {
		t.Fatal(it, e)
	}
}
func TestPublicNextData(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-jike-access-token") != "" {
			t.Fatal("guest leaked token")
		}
		fmt.Fprint(w, `<html><script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"episode":{"eid":"64db2d493fa4090b744c313c","title":"公开单集","enclosure":{"url":"https://media.xyzcdn.net/x.m4a"}}}}}</script></html>`)
	})
	it, e := c.Detail(context.Background(), "", "episode", "64db2d493fa4090b744c313c")
	if e != nil || it.Title != "公开单集" || it.MediaURL == "" {
		t.Fatal(it, e)
	}
}
