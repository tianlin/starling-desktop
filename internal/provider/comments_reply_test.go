package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"starling/internal/model"
	"strings"
	"testing"
	"time"
)

const replyID = "66469a168029f4442e95a226"

func replyFixture(target string) string {
	return strings.TrimSuffix(strings.Replace(commentFixture, `"id":"`+commentID+`"`, `"id":"`+replyID+`"`, 1), "}") + `,"thread":"` + commentID + `","replyToComment":{"id":"` + target + `","owner":{"id":"` + commentEpisode + `","type":"EPISODE"},"author":{"nickname":"Target"},"text":"` + strings.Repeat("评", 210) + `"}}`
}

func TestReplyCommentFailureDoesNotReplay(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		calls := 0
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(status) }))
		c := newClient(s.Client(), s.URL, s.URL, s.URL)
		_, e := c.ReplyComment(context.Background(), "token", commentEpisode, "private draft", commentID, commentID)
		s.Close()
		want := map[int]string{401: "UNAUTHORIZED", 403: "FORBIDDEN", 429: "RATE_LIMIT"}[status]
		if !model.IsCode(e, want) || calls != 1 {
			t.Fatalf("status=%d calls=%d err=%v", status, calls, e)
		}
	}
	calls := 0
	entered := make(chan struct{})
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; close(entered); <-release }))
	defer s.Close()
	c := newClient(s.Client(), s.URL, s.URL, s.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, e := c.ReplyComment(ctx, "token", commentEpisode, "private draft", commentID, commentID)
		done <- e
	}()
	<-entered
	e := <-done
	close(release)
	if !model.IsCode(e, "COMMENT_UNCERTAIN") || calls != 1 || strings.Contains(e.Error(), "private draft") {
		t.Fatalf("calls=%d error=%v", calls, e)
	}
}

func TestCommentThreadRejectsDifferentPrimary(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[` + replyFixture(commentID) + `],"totalCount":1}`))
	}))
	defer s.Close()
	c := newClient(s.Client(), s.URL, s.URL, s.URL)
	if _, e := c.CommentThread(context.Background(), "token", commentEpisode, "66469a168029f4442e95a227", ""); !model.IsCode(e, "BAD_RESPONSE") {
		t.Fatalf("wrong primary accepted: %v", e)
	}
	missing := strings.Replace(replyFixture(commentID), `,"thread":"`+commentID+`"`, "", 1)
	p, e := decodeCommentPage([]byte(`{"data":[`+missing+`]}`), commentEpisode, true)
	if e != nil || p.Items[0].PrimaryCommentID != "" {
		t.Fatalf("missing relation should stay readable: %+v %v", p, e)
	}
	wrongOwner := strings.Replace(replyFixture(commentID), `"owner":{"id":"`+commentEpisode+`","type":"EPISODE"},"author":{"nickname"`, `"owner":{"id":"66467d2c251bd96e6cdcdddf","type":"EPISODE"},"author":{"nickname"`, 1)
	if _, e := decodeCommentPage([]byte(`{"data":[`+wrongOwner+`]}`), commentEpisode, true); e == nil {
		t.Fatal("foreign reply reference accepted")
	}
}
func TestReplyCommentTargets(t *testing.T) {
	for _, target := range []string{commentID, "66469a168029f4442e95a227"} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]json.RawMessage
				_ = json.NewDecoder(r.Body).Decode(&body)
				var got string
				_ = json.Unmarshal(body["replyToCommentId"], &got)
				if got != target || len(body) != 3 || r.URL.Path != "/v1/comment/create" {
					t.Error("wrong reply body")
				}
				_, _ = w.Write([]byte(`{"data":` + replyFixture(target) + `}`))
			}))
			defer s.Close()
			c := newClient(s.Client(), s.URL, s.URL, s.URL)
			got, e := c.ReplyComment(context.Background(), "token", commentEpisode, "reply", target, commentID)
			if e != nil || calls != 1 || got.PrimaryCommentID != commentID || got.ReplyTo == nil || got.ReplyTo.ID != target || len([]rune(got.ReplyTo.Summary)) != 200 {
				t.Fatalf("reply=%+v err=%v calls=%d", got, e, calls)
			}
		})
	}
}
func TestReplyCommentWrongResponseIsUncertain(t *testing.T) {
	for _, raw := range []string{commentFixture, replyFixture("66469a168029f4442e95a227"), strings.Replace(replyFixture(commentID), `"thread":"`+commentID+`"`, `"thread":"66469a168029f4442e95a227"`, 1)} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"data":` + raw + `}`)) }))
		c := newClient(s.Client(), s.URL, s.URL, s.URL)
		_, e := c.ReplyComment(context.Background(), "token", commentEpisode, "reply", commentID, commentID)
		s.Close()
		if !model.IsCode(e, "COMMENT_UNCERTAIN") {
			t.Fatalf("bad response: %v", e)
		}
	}
	if _, e := decodeCreatedComment([]byte(`{"data":`+replyFixture(commentID)+`}`), commentEpisode); e == nil {
		t.Fatal("root create accepted reply")
	}
}
func TestCommentRelationshipRead(t *testing.T) {
	p, e := decodeCommentPage([]byte(`{"data":[`+replyFixture(commentID)+`],"totalCount":1}`), commentEpisode, true)
	if e != nil || p.Items[0].PrimaryCommentID != commentID || p.Items[0].ReplyTo.Nickname != "Target" {
		t.Fatalf("%+v %v", p, e)
	}
	for _, raw := range []string{strings.Replace(replyFixture(commentID), `"thread":"`+commentID+`"`, `"thread":"../invalid"`, 1), strings.Replace(replyFixture(commentID), `"id":"`+commentID+`","owner"`, `"id":"bad","owner"`, 1)} {
		if _, e := decodeCommentPage([]byte(`{"data":[`+raw+`]}`), commentEpisode, true); e == nil {
			t.Fatal("invalid relation accepted")
		}
	}
}
