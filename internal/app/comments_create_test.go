package app

import (
	"context"
	"encoding/json"
	"starling/internal/model"
	"starling/internal/testkit"
	"strings"
	"testing"
)

func createdFixture() model.Comment {
	return model.Comment{ID: idB, Author: model.Identity{ID: "user-a", Nickname: "合成测试账号"}, Text: "合成发表", CreatedAt: "2026-09-12T08:00:00Z"}
}

func TestCommentCreateRejectsInvalidBeforeProvider(t *testing.T) {
	calls := 0
	s := fixture(t, &testkit.Fake{CreateCommentFunc: func(context.Context, string, string, string) (model.Comment, error) {
		calls++
		return createdFixture(), nil
	}})
	epoch := connect(t, s)
	for _, args := range [][3]string{{"bad", "text", "request-1"}, {idA, " \n\t ", "request-2"}, {idA, "text", ""}} {
		if _, e := s.CreateComment(context.Background(), epoch, args[0], args[1], args[2]); e == nil {
			t.Fatal("invalid request accepted", args)
		}
	}
	if _, e := s.CreateComment(context.Background(), epoch-1, idA, "text", "request-3"); !model.IsCode(e, "STALE_SESSION") {
		t.Fatal(e)
	}
	if calls != 0 {
		t.Fatal(calls)
	}
}

func TestCommentCreateOneShotAndSerialAdmission(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	calls := 0
	s := fixture(t, &testkit.Fake{CreateCommentFunc: func(context.Context, string, string, string) (model.Comment, error) {
		calls++
		close(entered)
		<-release
		return createdFixture(), nil
	}})
	epoch := connect(t, s)
	done := make(chan error, 1)
	go func() {
		_, err := s.CreateComment(context.Background(), epoch, idA, "合成发表", "request-1")
		done <- err
	}()
	<-entered
	if _, e := s.CreateComment(context.Background(), epoch, idA, "合成发表", "request-1"); !model.IsCode(e, "COMMENT_DUPLICATE") {
		t.Fatal(e)
	}
	if _, e := s.CreateComment(context.Background(), epoch, idA, "other", "request-2"); !model.IsCode(e, "COMMENT_BUSY") {
		t.Fatal(e)
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateComment(context.Background(), epoch, idA, "合成发表", "request-1"); !model.IsCode(e, "COMMENT_DUPLICATE") {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}

func TestCommentUncertainPreservesTombstoneAndPrivateDiagnostics(t *testing.T) {
	calls := 0
	s := fixture(t, &testkit.Fake{CreateCommentFunc: func(context.Context, string, string, string) (model.Comment, error) {
		calls++
		return model.Comment{}, model.Err("COMMENT_UNCERTAIN", "发表结果未确认。")
	}})
	epoch := connect(t, s)
	args := `{"epoch":` + jsonNumber(epoch) + `,"episodeId":"` + idA + `","text":"private draft fixture","requestId":"request-1"}`
	if raw := s.Dispatch(context.Background(), "comments.create", args); !strings.Contains(raw, "COMMENT_UNCERTAIN") {
		t.Fatal(raw)
	}
	if raw := s.Dispatch(context.Background(), "comments.create", args); !strings.Contains(raw, "COMMENT_DUPLICATE") {
		t.Fatal(raw)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	raw := s.Dispatch(context.Background(), "diagnostics", `{}`)
	if strings.Contains(raw, "private draft") || strings.Contains(raw, idA) || strings.Contains(raw, "user-a") {
		t.Fatal("private data in diagnostics")
	}
}

func TestCommentCreateRejectsDifferentAuthorAndLateAccount(t *testing.T) {
	s := fixture(t, &testkit.Fake{CreateCommentFunc: func(context.Context, string, string, string) (model.Comment, error) {
		v := createdFixture()
		v.Author.ID = "other-account"
		return v, nil
	}})
	epoch := connect(t, s)
	if _, e := s.CreateComment(context.Background(), epoch, idA, "text", "request-1"); !model.IsCode(e, "COMMENT_UNCERTAIN") {
		t.Fatal(e)
	}
	// A delayed provider success cannot show another account's data through Dispatch.
	f := s.p.(*testkit.Fake)
	f.CreateCommentFunc = func(context.Context, string, string, string) (model.Comment, error) {
		if e := s.Logout(); e != nil {
			t.Fatal(e)
		}
		return createdFixture(), nil
	}
	raw := s.Dispatch(context.Background(), "comments.create", `{"epoch":`+jsonNumber(epoch)+`,"episodeId":"`+idA+`","text":"text","requestId":"request-2"}`)
	var result struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	if e := json.Unmarshal([]byte(raw), &result); e != nil || result.OK || len(result.Data) != 0 || !strings.Contains(raw, "STALE_SESSION") {
		t.Fatal(raw, e)
	}
}
