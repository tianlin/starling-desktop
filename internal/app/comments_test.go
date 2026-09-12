package app

import (
	"context"
	"encoding/json"
	"starling/internal/model"
	"starling/internal/testkit"
	"testing"
)

func TestCommentsRequireConnectedCurrentSession(t *testing.T) {
	calls := 0
	f := &testkit.Fake{CommentsFunc: func(context.Context, string, string, string) (model.CommentPage, error) {
		calls++
		return model.CommentPage{Items: []model.Comment{}, Complete: true}, nil
	}}
	s := fixture(t, f)
	if _, err := s.Comments(context.Background(), s.Session().Epoch, idA, "", ""); err == nil {
		t.Fatal("guest read allowed")
	}
	epoch := connect(t, s)
	if _, err := s.Comments(context.Background(), epoch-1, idA, "", ""); !model.IsCode(err, "STALE_SESSION") {
		t.Fatal(err)
	}
	if _, err := s.Comments(context.Background(), epoch, "bad", "", ""); !model.IsCode(err, "INVALID_ID") {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("invalid requests reached provider")
	}
	var result struct {
		OK   bool              `json:"ok"`
		Data model.CommentPage `json:"data"`
	}
	raw := s.Dispatch(context.Background(), "comments.list", `{"epoch":`+jsonNumber(epoch)+`,"episodeId":"`+idA+`","cursor":""}`)
	if err := json.Unmarshal([]byte(raw), &result); err != nil || !result.OK || !result.Data.Complete || calls != 1 {
		t.Fatal(raw, err, calls)
	}
}

func jsonNumber(v uint64) string { b, _ := json.Marshal(v); return string(b) }

func TestCommentsDiscardResponseAfterLogout(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	f := &testkit.Fake{CommentsFunc: func(context.Context, string, string, string) (model.CommentPage, error) {
		close(entered)
		<-release
		return model.CommentPage{Items: []model.Comment{{ID: idA, Text: "private fixture"}}, Complete: true}, nil
	}}
	s := fixture(t, f)
	epoch := connect(t, s)
	done := make(chan error, 1)
	go func() { _, err := s.Comments(context.Background(), epoch, idA, "", ""); done <- err }()
	<-entered
	if err := s.Logout(); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; !model.IsCode(err, "STALE_SESSION") {
		t.Fatal(err)
	}
}

func TestCommentsThreadAndReadRefresh(t *testing.T) {
	calls := 0
	f := &testkit.Fake{CommentThreadFunc: func(_ context.Context, token, eid, cid, cursor string) (model.CommentPage, error) {
		calls++
		if eid != idA || cid != idB || cursor != "" {
			t.Fatal(eid, cid, cursor)
		}
		if token == "old" {
			return model.CommentPage{}, model.Err("UNAUTHORIZED", "expired")
		}
		return model.CommentPage{Items: []model.Comment{{ID: idB, Text: "reply"}}, Complete: true}, nil
	}}
	s := fixture(t, f)
	epoch := connect(t, s)
	out, err := s.Comments(context.Background(), epoch, idA, idB, "")
	if err != nil || calls != 2 || len(out.Items) != 1 {
		t.Fatal(out, err, calls)
	}
}
