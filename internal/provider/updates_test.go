package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"starling/internal/model"
	"testing"
	"time"
)

func TestUpdatesRequestAndCursor(t *testing.T) {
	calls := 0
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/inbox/list" || r.Method != "POST" {
			t.Error("wrong endpoint")
		}
		if r.Header.Get("User-Agent") != userAgent || r.Header.Get("Model") != "" || r.Header.Get("x-jike-access-token") != "fixture" {
			t.Error("wrong identity")
		}
		var body map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		if string(body["limit"]) != `"20"` {
			t.Error("wrong limit")
		}
		if calls == 1 {
			if _, ok := body["loadMoreKey"]; ok {
				t.Error("first page cursor")
			}
			fmt.Fprint(w, `{"data":[{"episode":{"eid":"64db2d493fa4090b744c313c","title":"更新","pubDate":"2026-09-12T01:00:00Z","podcast":{"pid":"64db2d493fa4090b744c313d","title":"播客"}}}],"loadMoreKey":{"pubDate":"2026-09-12T01:00:00Z","id":"64db2d493fa4090b744c313c"}}`)
		} else {
			var key map[string]string
			json.Unmarshal(body["loadMoreKey"], &key)
			if key["id"] != "64db2d493fa4090b744c313c" || key["pubDate"] != "2026-09-12T01:00:00Z" {
				t.Error("cursor changed")
			}
			fmt.Fprint(w, `{"data":[],"loadMoreKey":null}`)
		}
	})
	p, e := c.List(context.Background(), "fixture", "updates", "", "")
	if e != nil || len(p.Items) != 1 || p.Complete || p.Cursor == "" {
		t.Fatalf("first page: %+v %v", p, e)
	}
	if p.Items[0].Kind != "episode" || p.Items[0].PodcastTitle != "播客" {
		t.Fatal("wrong item")
	}
	p, e = c.List(context.Background(), "fixture", "updates", "", p.Cursor)
	if e != nil || !p.Complete || calls != 2 {
		t.Fatalf("last page: %+v %v", p, e)
	}
}

func TestUpdatesConservativePagination(t *testing.T) {
	for _, tc := range []struct {
		body    string
		failure bool
	}{
		{`{"data":[]}`, false},
		{`{"data":[],"hasMore":true}`, true},
		{`{"data":[],"loadMoreKey":{"pubDate":"2026-09-12T01:00:00Z","id":"64db2d493fa4090b744c313c"}}`, true},
		{`{"data":[{"eid":"64db2d493fa4090b744c313c"}],"hasMore":false,"loadMoreKey":{"pubDate":"2026-09-12T01:00:00Z","id":"64db2d493fa4090b744c313c"}}`, true},
		{`{"data":[{"eid":"64db2d493fa4090b744c313c"}],"loadMoreKey":{"offset":1}}`, true},
		{`{"data":null}`, true},
	} {
		c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) })
		p, e := c.List(context.Background(), "fixture", "updates", "", "")
		if (e != nil) != tc.failure || p.Complete {
			t.Errorf("body %s complete %v err %v", tc.body, p.Complete, e)
		}
	}
}

func TestUpdatesRejectInvalidCursorBeforeNetwork(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") })
	for _, raw := range []string{`"text"`, `{"id":"bad","pubDate":"yesterday"}`, `{"offset":1}`} {
		_, e := c.List(context.Background(), "fixture", "updates", "", EncodeCursor(json.RawMessage(raw)))
		if !model.IsCode(e, "BAD_CURSOR") {
			t.Errorf("wrong error: %v", e)
		}
	}
	_, e := c.List(context.Background(), "", "updates", "", "")
	if !model.IsCode(e, "UNAUTHORIZED") {
		t.Fatal(e)
	}
}

func TestUpdatesHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{401, "UNAUTHORIZED"}, {429, "RATE_LIMIT"}} {
		calls := 0
		c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(tc.status) })
		_, e := c.List(context.Background(), "fixture", "updates", "", "")
		if !model.IsCode(e, tc.code) || calls != 1 {
			t.Fatalf("%d: %v calls=%d", tc.status, e, calls)
		}
	}
}

func TestUpdatesDeadlineCancelsRead(t *testing.T) {
	started := make(chan struct{})
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, e := c.List(ctx, "fixture", "updates", "", "")
	if !model.IsCode(e, "CANCELLED") {
		t.Fatal(e)
	}
	select {
	case <-started:
	default:
		t.Fatal("request did not reach the fixture")
	}
}
