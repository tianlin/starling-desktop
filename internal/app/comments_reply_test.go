package app

import (
	"context"
	"starling/internal/model"
	"starling/internal/testkit"
	"testing"
)

const replyID = "64db2d493fa4090b744c5199"

func replyReader() *testkit.Fake {
	return &testkit.Fake{
		CommentsFunc: func(context.Context, string, string, string) (model.CommentPage, error) {
			return model.CommentPage{Items: []model.Comment{{ID: idB}}, Complete: true}, nil
		},
		CommentThreadFunc: func(context.Context, string, string, string, string) (model.CommentPage, error) {
			return model.CommentPage{Items: []model.Comment{{ID: replyID, PrimaryCommentID: idB, ReplyTo: &model.CommentReference{ID: idB}}}, Complete: true}, nil
		},
	}
}
func TestReplyRequiresReadTargetInCurrentEpisodeAndEpoch(t *testing.T) {
	ctx := context.Background()
	f := replyReader()
	calls := 0
	f.ReplyCommentFunc = func(_ context.Context, _, _, _, target, primary string) (model.Comment, error) {
		calls++
		v := createdFixture()
		v.PrimaryCommentID = primary
		v.ReplyTo = &model.CommentReference{ID: target}
		return v, nil
	}
	s := fixture(t, f)
	epoch := connect(t, s)
	reject := func(eid, target, primary string) {
		t.Helper()
		_, err := s.CreateCommentReply(ctx, epoch, eid, "text", "reply-request", target, primary)
		if !model.IsCode(err, "COMMENT_TARGET_INVALID") {
			t.Fatal(err)
		}
	}
	reject(idA, idB, idB)
	s.Comments(ctx, epoch, idA, "", "")
	reject(idA, replyID, idB)
	reject(idB, idB, idB)
	reject(idA, idB, replyID)
	reject(idA, idB, "")
	if calls != 0 {
		t.Fatal(calls)
	}
	for i, target := range []string{idB, replyID} {
		if i == 1 {
			s.Comments(ctx, epoch, idA, idB, "")
		}
		out, err := s.CreateCommentReply(ctx, epoch, idA, "text", "reply-request-"+target, target, idB)
		if err != nil || out.Comment.PrimaryCommentID != idB || out.Comment.ReplyTo.ID != target {
			t.Fatal(out, err)
		}
	}
	s.Logout()
	epoch = connect(t, s)
	reject(idA, idB, idB)
	if calls != 2 {
		t.Fatal(calls)
	}
}
func TestReplyOneShotAndMismatchedResponse(t *testing.T) {
	for _, code := range []string{"UNAUTHORIZED", "COMMENT_UNCERTAIN", "mismatch"} {
		t.Run(code, func(t *testing.T) {
			ctx := context.Background()
			f := replyReader()
			calls, refresh := 0, 0
			f.RefreshFunc = func(context.Context, model.Credentials) (model.Credentials, error) {
				refresh++
				return model.Credentials{}, nil
			}
			f.ReplyCommentFunc = func(context.Context, string, string, string, string, string) (model.Comment, error) {
				calls++
				if code == "mismatch" {
					return createdFixture(), nil
				}
				return model.Comment{}, model.Err(code, "fixture")
			}
			s := fixture(t, f)
			epoch := connect(t, s)
			s.Comments(ctx, epoch, idA, "", "")
			_, err := s.CreateCommentReply(ctx, epoch, idA, "text", "reply-request", idB, idB)
			want := code
			if code == "mismatch" {
				want = "COMMENT_UNCERTAIN"
			}
			if !model.IsCode(err, want) {
				t.Fatal(err)
			}
			s.CreateCommentReply(ctx, epoch, idA, "text", "reply-request", idB, idB)
			if calls != 1 || refresh != 0 {
				t.Fatal(calls, refresh)
			}
		})
	}
}

func TestReplySerialAdmissionAndLateLogout(t *testing.T) {
	ctx := context.Background()
	f := replyReader()
	entered, release := make(chan struct{}), make(chan struct{})
	calls := 0
	f.ReplyCommentFunc = func(context.Context, string, string, string, string, string) (model.Comment, error) {
		calls++
		close(entered)
		<-release
		v := createdFixture()
		v.PrimaryCommentID = idB
		v.ReplyTo = &model.CommentReference{ID: idB}
		return v, nil
	}
	s := fixture(t, f)
	epoch := connect(t, s)
	s.Comments(ctx, epoch, idA, "", "")
	done := make(chan error, 1)
	go func() {
		_, err := s.CreateCommentReply(ctx, epoch, idA, "text", "reply-request", idB, idB)
		done <- err
	}()
	<-entered
	if _, err := s.CreateCommentReply(ctx, epoch, idA, "text", "reply-request", idB, idB); !model.IsCode(err, "COMMENT_DUPLICATE") {
		t.Fatal(err)
	}
	if _, err := s.CreateComment(ctx, epoch, idA, "root", "another-request"); !model.IsCode(err, "COMMENT_BUSY") {
		t.Fatal(err)
	}
	s.Logout()
	close(release)
	if err := <-done; !model.IsCode(err, "STALE_SESSION") {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.commentTargets) != 0 {
		t.Fatal("logout retained reply index")
	}
}

func TestUnrelatedThreadReadCannotAuthorizeReply(t *testing.T) {
	ctx := context.Background()
	f := replyReader()
	f.CommentThreadFunc = func(context.Context, string, string, string, string) (model.CommentPage, error) {
		return model.CommentPage{Items: []model.Comment{{ID: replyID, PrimaryCommentID: idA}}, Complete: true}, nil
	}
	s := fixture(t, f)
	epoch := connect(t, s)
	s.Comments(ctx, epoch, idA, "", "")
	s.Comments(ctx, epoch, idA, idB, "")
	if _, err := s.CreateCommentReply(ctx, epoch, idA, "text", "reply-request", replyID, idB); !model.IsCode(err, "COMMENT_TARGET_INVALID") {
		t.Fatal(err)
	}
}
