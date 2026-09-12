package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"starling/internal/model"
	"testing"
)

const commentEpisode = "66467d2c251bd96e6cdcddde"
const commentID = "66468a29aa440ecaaa8db490"
const commentFixture = `{"id":"66468a29aa440ecaaa8db490","owner":{"id":"66467d2c251bd96e6cdcddde","type":"EPISODE"},"author":{"uid":"65f8d456edce67104a48c738","nickname":"Synthetic user"},"text":"<b>plain text</b>","createdAt":"2024-05-16T22:35:21.138Z","threadReplyCount":2}`

func TestCommentsReadContract(t *testing.T) {
	calls := 0
	key := `{"direction":"NEXT","hotSortScore":0.9,"id":"66468a29aa440ecaaa8db490","section":""}`
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Fatal("invalid body")
		}
		if r.Method != "POST" || r.URL.Path != "/v1/comment/list-primary" || r.Header.Get("x-jike-access-token") != "test-token" || r.UserAgent() != userAgent {
			t.Error("request contract mismatch")
		}
		var owner map[string]string
		_ = json.Unmarshal(body["owner"], &owner)
		if owner["id"] != commentEpisode || owner["type"] != "EPISODE" || string(body["order"]) != `"HOT"` {
			t.Error("wrong owner/order")
		}
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"data":[` + commentFixture + `],"loadMoreKey":` + key + `}`))
		} else {
			if string(body["loadMoreKey"]) != key {
				var a, b any
				_ = json.Unmarshal(body["loadMoreKey"], &a)
				_ = json.Unmarshal([]byte(key), &b)
				aa, _ := json.Marshal(a)
				bb, _ := json.Marshal(b)
				if string(aa) != string(bb) {
					t.Error("cursor not roundtripped")
				}
			}
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	defer s.Close()
	c := newClient(s.Client(), s.URL, s.URL, s.URL)
	p, e := c.Comments(context.Background(), "test-token", commentEpisode, "")
	if e != nil || p.Complete || p.Cursor == "" || len(p.Items) != 1 {
		t.Fatalf("first page: %+v %v", p, e)
	}
	if p.Items[0].Text != "<b>plain text</b>" || p.Items[0].ReplyCount != 2 || p.Items[0].Author.Nickname != "Synthetic user" {
		t.Fatal("normalization mismatch")
	}
	p, e = c.Comments(context.Background(), "test-token", commentEpisode, p.Cursor)
	if e != nil || !p.Complete || len(p.Items) != 0 {
		t.Fatalf("terminal page: %+v %v", p, e)
	}
}

func TestCommentThreadContract(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/v1/comment/list-thread" || body["order"] != "SMART" || body["primaryCommentId"] != commentID || len(body) != 2 {
			t.Error("thread request mismatch")
		}
		_, _ = w.Write([]byte(`{"data":[` + commentFixture + `],"totalCount":1}`))
	}))
	defer s.Close()
	c := newClient(s.Client(), s.URL, s.URL, s.URL)
	p, e := c.CommentThread(context.Background(), "test-token", commentEpisode, commentID, "")
	if e != nil || !p.Complete || len(p.Items) != 1 {
		t.Fatalf("thread: %+v %v", p, e)
	}
	_, e = c.CommentThread(context.Background(), "test-token", commentEpisode, commentID, EncodeCursor(json.RawMessage(`{"id":"next"}`)))
	if !model.IsCode(e, "UNSUPPORTED") {
		t.Fatalf("expected unsupported thread pagination: %v", e)
	}
}

func TestCommentsRejectInvalidAndIncomplete(t *testing.T) {
	c := newClient(&http.Client{}, "http://invalid", "http://invalid", "http://invalid")
	if _, e := c.Comments(context.Background(), "", commentEpisode, ""); !model.IsCode(e, "UNAUTHORIZED") {
		t.Fatal(e)
	}
	if _, e := c.Comments(context.Background(), "token", "../bad", ""); !model.IsCode(e, "INVALID_ID") {
		t.Fatal(e)
	}
	if _, e := c.Comments(context.Background(), "token", commentEpisode, EncodeCursor(json.RawMessage(`"foreign"`))); !model.IsCode(e, "BAD_CURSOR") {
		t.Fatal(e)
	}
	for _, raw := range []string{`{"data":null}`, `{"data":[{}]}`, `{"data":[` + commentFixture + `],"loadMoreKey":{"unexpected":1}}`, `{"data":[],"totalCount":-1}`} {
		if _, e := decodeCommentPage([]byte(raw), commentEpisode, false); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	p, e := decodeCommentPage([]byte(`{"data":[`+commentFixture+`],"totalCount":5}`), commentEpisode, true)
	if e != nil || p.Complete || p.Cursor != "" {
		t.Fatalf("partial thread falsely complete: %+v %v", p, e)
	}
	p, e = decodeCommentPage([]byte(`{"data":[]}`), commentEpisode, true)
	if e != nil || p.Complete {
		t.Fatalf("missing thread evidence: %+v %v", p, e)
	}
}

func TestCommentPaginationEvidence(t *testing.T) {
	p, e := decodeCommentPage([]byte(`{"data":[`+commentFixture+`],"loadMoreKey":"thread-next","totalCount":2}`), commentEpisode, true)
	if e != nil || p.Complete || p.Cursor != "" || len(p.Items) != 1 {
		t.Fatalf("unsupported thread continuation should stay partial: %+v %v", p, e)
	}
	if _, e := decodeCommentPage([]byte(`{"data":[`+commentFixture+`],"loadNextKey":{"id":"next"}}`), commentEpisode, false); !model.IsCode(e, "PAGINATION") {
		t.Fatalf("unsupported next key was treated as terminal: %v", e)
	}
}
