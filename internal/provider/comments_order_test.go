package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"starling/internal/model"
	"strings"
	"testing"
)

func TestCommentsLatestPaginationAndScope(t *testing.T) {
	calls := 0
	key := `{"id":"66468a29aa440ecaaa8db490","direction":"NEXT","createdAt":"2024-05-16T22:35:21.138Z"}`
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		if string(body["order"]) != `"TIME"` {
			t.Error("latest must send TIME")
		}
		if calls == 1 {
			_, _ = w.Write([]byte(`{"data":[` + commentFixture + `],"loadMoreKey":` + key + `}`))
			return
		}
		var got, want any
		_ = json.Unmarshal(body["loadMoreKey"], &got)
		_ = json.Unmarshal([]byte(key), &want)
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(want)
		if string(a) != string(b) {
			t.Error("TIME key not preserved")
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer s.Close()
	c := newClient(s.Client(), s.URL, s.URL, s.URL)
	p, e := c.CommentsOrdered(context.Background(), "token", commentEpisode, "", model.CommentOrderLatest)
	if e != nil || p.Cursor == "" || p.Complete {
		t.Fatalf("latest first page: %+v %v", p, e)
	}
	for _, tt := range []struct {
		eid   string
		order model.CommentOrder
	}{{commentEpisode, model.CommentOrderHot}, {"66467d2c251bd96e6cdcdddf", model.CommentOrderLatest}} {
		if _, e := c.CommentsOrdered(context.Background(), "token", tt.eid, p.Cursor, tt.order); !model.IsCode(e, "BAD_CURSOR") {
			t.Fatalf("cross-scope cursor accepted: %v", e)
		}
	}
	if calls != 1 {
		t.Fatal("scope mismatch made a request")
	}
	p, e = c.CommentsOrdered(context.Background(), "token", commentEpisode, p.Cursor, model.CommentOrderLatest)
	if e != nil || !p.Complete || calls != 2 {
		t.Fatalf("next page: %+v %v calls=%d", p, e, calls)
	}
}

func TestCommentOrderValidation(t *testing.T) {
	if got, e := model.NormalizeCommentOrder(""); e != nil || got != model.CommentOrderHot {
		t.Fatalf("default=%q %v", got, e)
	}
	c := newClient(&http.Client{}, "http://invalid", "http://invalid", "http://invalid")
	if _, e := c.CommentsOrdered(context.Background(), "token", commentEpisode, "", "other"); !model.IsCode(e, "INVALID_ORDER") {
		t.Fatal(e)
	}
	for _, raw := range []string{`{"id":"66468a29aa440ecaaa8db490","direction":"NEXT"}`, `{"episodeID":"66467d2c251bd96e6cdcddde","order":"latest","raw":{"id":"bad"}}`} {
		if _, e := c.CommentsOrdered(context.Background(), "token", commentEpisode, EncodeCursor(json.RawMessage(raw)), model.CommentOrderLatest); !model.IsCode(e, "BAD_CURSOR") {
			t.Fatal(e)
		}
	}
	if _, e := c.CommentsOrdered(context.Background(), "token", commentEpisode, strings.Repeat("a", 8193), model.CommentOrderHot); !model.IsCode(e, "BAD_CURSOR") {
		t.Fatal(e)
	}
}

func TestCommentCursorOrderShape(t *testing.T) {
	for _, raw := range []string{`{"id":"66468a29aa440ecaaa8db490","direction":"NEXT","hotSortScore":null}`, `{"id":"66468a29aa440ecaaa8db490","direction":"NEXT","hotSortScore":"x"}`} {
		if validCommentCursor(json.RawMessage(raw), model.CommentOrderHot) {
			t.Fatal("invalid HOT score accepted")
		}
	}
	timeKey := json.RawMessage(`{"id":"66468a29aa440ecaaa8db490","direction":"NEXT","section":""}`)
	if !validCommentCursor(timeKey, model.CommentOrderLatest) || validCommentCursor(timeKey, model.CommentOrderHot) {
		t.Fatal("order-specific key contract ignored")
	}
}
