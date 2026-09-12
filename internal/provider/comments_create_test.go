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

func TestCreateCommentContract(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Method != "POST" || r.URL.Path != "/v1/comment/create" || r.UserAgent() != userAgent || r.Header.Get("x-jike-access-token") != "test-token" || len(body) != 2 {
			t.Error("create request contract mismatch")
		}
		var owner map[string]string
		_ = json.Unmarshal(body["owner"], &owner)
		var text string
		_ = json.Unmarshal(body["text"], &text)
		if owner["id"] != commentEpisode || owner["type"] != "EPISODE" || text != "  测试\n评论  " {
			t.Error("create body mismatch")
		}
		_, _ = w.Write([]byte(`{"data":` + strings.Replace(commentFixture, `"threadReplyCount":2`, `"replyCount":3`, 1) + `,"toast":"ignored"}`))
	}))
	defer s.Close()
	c := newClient(s.Client(), s.URL, s.URL, s.URL)
	got, e := c.CreateComment(context.Background(), "test-token", commentEpisode, "  测试\n评论  ")
	if e != nil || got.ID != commentID || got.ReplyCount != 3 || got.Text != "<b>plain text</b>" || calls != 1 {
		t.Fatalf("create result=%+v error=%v calls=%d", got, e, calls)
	}
}

func TestCreateCommentNoRetryAndSafeFailures(t *testing.T) {
	for _, tt := range []struct {
		name       string
		status     int
		body, code string
	}{
		{"unauthorized", 401, `{"text":"private-draft"}`, "UNAUTHORIZED"},
		{"forbidden", 403, `{}`, "FORBIDDEN"},
		{"rate", 429, `{}`, "RATE_LIMIT"},
		{"business", 200, `{"code":123,"message":"private-draft"}`, "REQUEST_REJECTED"},
		{"upstream", 503, `{}`, "COMMENT_UNCERTAIN"},
		{"malformed", 200, `{"data":{}}`, "COMMENT_UNCERTAIN"},
		{"redirect", 302, `{}`, "COMMENT_UNCERTAIN"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer s.Close()
			c := newClient(s.Client(), s.URL, s.URL, s.URL)
			_, e := c.CreateComment(context.Background(), "test-token", commentEpisode, "private-draft")
			if !model.IsCode(e, tt.code) || calls != 1 || strings.Contains(e.Error(), "private-draft") {
				t.Fatalf("error=%v calls=%d", e, calls)
			}
		})
	}
	t.Run("network", func(t *testing.T) {
		calls := 0
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			conn, _, e := w.(http.Hijacker).Hijack()
			if e != nil {
				t.Error(e)
				return
			}
			_ = conn.Close()
		}))
		defer s.Close()
		c := newClient(s.Client(), s.URL, s.URL, s.URL)
		_, e := c.CreateComment(context.Background(), "test-token", commentEpisode, "private-draft")
		if !model.IsCode(e, "COMMENT_UNCERTAIN") || calls != 1 {
			t.Fatalf("error=%v calls=%d", e, calls)
		}
	})
}

func TestCreateCommentPreflightAndResponseValidation(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer s.Close()
	c := newClient(s.Client(), s.URL, s.URL, s.URL)
	for _, text := range []string{"", " \n\t\u3000", "a\x00b", string([]byte{0xff}), strings.Repeat("a", (1<<20)+1)} {
		if _, e := c.CreateComment(context.Background(), "test-token", commentEpisode, text); !model.IsCode(e, "INVALID_COMMENT") {
			t.Fatalf("invalid text error: %v", e)
		}
	}
	if _, e := c.CreateComment(context.Background(), "", commentEpisode, "ok"); !model.IsCode(e, "UNAUTHORIZED") {
		t.Fatal(e)
	}
	if _, e := c.CreateComment(context.Background(), "test-token", "../bad", "ok"); !model.IsCode(e, "INVALID_ID") {
		t.Fatal(e)
	}
	if calls != 0 {
		t.Fatalf("preflight sent %d requests", calls)
	}
	for _, raw := range []string{`{"data":` + strings.ReplaceAll(commentFixture, commentEpisode, "66467d2c251bd96e6cdcdddf") + `}`, `{"data":` + strings.Replace(commentFixture, `"threadReplyCount":2`, `"replyCount":-1`, 1) + `}`} {
		if _, e := decodeCreatedComment([]byte(raw), commentEpisode); e == nil {
			t.Fatal("invalid created comment accepted")
		}
	}
}

func TestCreateCommentCancellationAfterDispatch(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; close(entered); <-release }))
	defer s.Close()
	c := newClient(s.Client(), s.URL, s.URL, s.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := c.CreateComment(ctx, "test-token", commentEpisode, "private-draft"); done <- e }()
	<-entered
	cancel()
	e := <-done
	close(release)
	if !model.IsCode(e, "COMMENT_UNCERTAIN") || calls != 1 {
		t.Fatalf("error=%v calls=%d", e, calls)
	}
}

func TestCommentTextHasNoInventedCharacterLimit(t *testing.T) {
	if e := model.ValidateCommentText(strings.Repeat("评", 10000)); e != nil {
		t.Fatalf("valid long Unicode text rejected: %v", e)
	}
}
