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
