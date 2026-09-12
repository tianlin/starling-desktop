package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
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

func TestLibraryDeviceSessionIsStableAndDistinct(t *testing.T) {
	var devices []string
	var bodies []map[string]any
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("x-jike-device-id")
		if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
			t.Errorf("missing valid client-generated UUID: %q", id)
		}
		devices = append(devices, id)
		if r.Header.Get("User-Agent") != userAgent || r.Header.Get("Model") != "" || r.Header.Get("Manufacturer") != "" {
			t.Error("must identify as Starling without a fabricated mobile fingerprint")
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		fmt.Fprint(w, `{"data":[{"eid":"64db2d493fa4090b744c313c","title":"收藏"}],"loadMoreKey":{"offset":10}}`)
	})
	p, e := c.List(context.Background(), "fixture-token", "favorites", "", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.List(context.Background(), "fixture-token", "favorites", "", p.Cursor); e != nil {
		t.Fatal(e)
	}
	other := newClient(c.http, c.api, c.auth, c.web)
	if _, e = other.List(context.Background(), "fixture-token", "favorites", "", ""); e != nil {
		t.Fatal(e)
	}
	if devices[0] != devices[1] || devices[0] == devices[2] {
		t.Fatal("device UUID must be stable per client, independent across clients")
	}
	if bodies[1]["loadMoreKey"].(map[string]any)["offset"] != float64(10) {
		t.Fatal("cursor not forwarded")
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

func TestPrivateLibraryOmittedTerminalCursor(t *testing.T) {
	for _, kind := range []string{"favorites", "subscriptions"} {
		t.Run(kind, func(t *testing.T) {
			for _, data := range []string{`[]`, `[{"eid":"64db2d493fa4090b744c313c","pid":"64db2d493fa4090b744c313d","title":"末页内容"}]`} {
				c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"data":%s}`, data) })
				p, e := c.List(context.Background(), "token", kind, "", EncodeCursor(json.RawMessage(`{"offset":10}`)))
				if e != nil || !p.Complete || p.Cursor != "" {
					t.Fatalf("terminal page: complete=%v err=%v", p.Complete, e)
				}
			}
		})
	}
}

func TestLibraryOmissionRuleDoesNotOverrideMalformedOrExplicitPagination(t *testing.T) {
	for _, body := range []string{`{"data":null}`, `{"data":{}}`, `{"data":[],"hasMore":true}`, `{"data":[],"loadMoreKey":[]}`} {
		c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, e := c.List(context.Background(), "token", "favorites", "", ""); e == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":[]}`) })
	p, e := c.List(context.Background(), "token", "episodes", "64db2d493fa4090b744c313c", "")
	if e != nil || p.Complete {
		t.Fatal("unverified endpoint must keep unknown end", e)
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

func TestRejectedRequestRetainsSafeStatusWithoutUpstreamMessage(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"code":1,"message":"private-token private-account"}`)
	})
	_, e := c.List(context.Background(), "fixture-token", "favorites", "", "")
	if !model.IsCode(e, "REQUEST_REJECTED") {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(model.PublicError(e))
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	if fields["httpStatus"] != float64(400) || fields["upstreamCode"] != float64(1) {
		t.Fatalf("missing safe status: %s", raw)
	}
	if strings.Contains(string(raw), "private-") {
		t.Fatal("upstream details leaked")
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
