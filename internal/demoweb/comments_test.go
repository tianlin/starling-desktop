package demoweb

import (
	"context"
	"starling/internal/model"
	"testing"
)

func TestSyntheticCommentsHaveMultiplePagesAndThread(t *testing.T) {
	s, err := New(t.TempDir(), "127.0.0.1:34115")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	settings := model.DefaultSettings()
	settings.ExperimentalAccount = true
	if err := s.app.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := s.app.Login(context.Background(), "00000000000", "+86", "0000", false); err != nil {
		t.Fatal(err)
	}
	epoch := s.app.Session().Epoch
	p, err := s.app.Comments(context.Background(), epoch, sample(0).ID, "", "")
	if err != nil || len(p.Items) != 2 || p.Complete || p.Cursor == "" {
		t.Fatal(p, err)
	}
	q, err := s.app.Comments(context.Background(), epoch, sample(0).ID, "", p.Cursor)
	if err != nil || !q.Complete || len(q.Items) != 2 || q.Items[0].ID != p.Items[1].ID {
		t.Fatal(q, err)
	}
	r, err := s.app.Comments(context.Background(), epoch, sample(0).ID, p.Items[0].ID, "")
	if err != nil || len(r.Items) != 1 || !r.Complete {
		t.Fatal(r, err)
	}
}

func TestSyntheticPublicationCanBeReadBack(t *testing.T) {
	s, err := New(t.TempDir(), "127.0.0.1:34115")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	settings := model.DefaultSettings()
	settings.ExperimentalAccount = true
	if err = s.app.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err = s.app.Login(context.Background(), "00000000000", "+86", "0000", false); err != nil {
		t.Fatal(err)
	}
	epoch := s.app.Session().Epoch
	out, err := s.app.CreateComment(context.Background(), epoch, sample(0).ID, "合成新增评论", "synthetic-request-1")
	if err != nil || out.Comment.Text != "合成新增评论" {
		t.Fatal(out, err)
	}
	p, err := s.app.Comments(context.Background(), epoch, sample(0).ID, "", "")
	if err != nil || len(p.Items) != 2 || p.Items[0].ID == out.Comment.ID {
		t.Fatal(p, err)
	}
	q, err := s.app.CommentsOrdered(context.Background(), epoch, sample(0).ID, "", "", model.CommentOrderLatest)
	if err != nil || len(q.Items) != 3 || q.Items[0].ID != out.Comment.ID {
		t.Fatal(q, err)
	}
	if _, err := s.app.CommentsOrdered(context.Background(), epoch, sample(0).ID, "", p.Cursor, model.CommentOrderLatest); err == nil {
		t.Fatal("mixed order cursor accepted")
	}
}

func TestSyntheticReplyReadBackStaysInThread(t *testing.T) {
	s, err := New(t.TempDir(), "127.0.0.1:34115")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg := model.DefaultSettings()
	cfg.ExperimentalAccount = true
	s.app.SaveSettings(cfg)
	ctx := context.Background()
	if err = s.app.Login(ctx, "00000000000", "+86", "0000", false); err != nil {
		t.Fatal(err)
	}
	epoch := s.app.Session().Epoch
	eid := sample(0).ID
	primary := syntheticComment(0).ID
	s.app.Comments(ctx, epoch, eid, "", "")
	s.app.Comments(ctx, epoch, eid, primary, "")
	for _, target := range []string{primary, syntheticComment(3).ID} {
		out, err := s.app.CreateCommentReply(ctx, epoch, eid, "合成回复读回", "request-"+target, target, primary)
		if err != nil {
			t.Fatal(err)
		}
		thread, err := s.app.Comments(ctx, epoch, eid, primary, "")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range thread.Items {
			if c.ID == out.Comment.ID {
				found = true
				if c.PrimaryCommentID != primary || c.ReplyTo.ID != target {
					t.Fatal(c)
				}
			}
		}
		if !found {
			t.Fatal("reply not read back")
		}
		roots, err := s.app.CommentsOrdered(ctx, epoch, eid, "", "", model.CommentOrderLatest)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range roots.Items {
			if c.ID == out.Comment.ID {
				t.Fatal("reply leaked into root list")
			}
		}
	}
}
