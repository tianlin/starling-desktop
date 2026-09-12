package app

import (
	"context"
	"starling/internal/model"
	"starling/internal/testkit"
	"testing"
)

func TestContentReadsRejectPreviousAccountEpochBeforeProvider(t *testing.T) {
	for _, operation := range []string{"detail", "library"} {
		t.Run(operation, func(t *testing.T) {
			calls := 0
			f := &testkit.Fake{}
			s := fixture(t, f)
			oldEpoch := connect(t, s)
			if err := s.Logout(); err != nil {
				t.Fatal(err)
			}
			f.LoginFunc = func(context.Context, string, string, string) (model.Credentials, model.Identity, error) {
				return model.Credentials{Access: "account-b", Refresh: "refresh-b"}, model.Identity{ID: "user-b"}, nil
			}
			f.MeFunc = func(context.Context, string) (model.Identity, error) {
				return model.Identity{ID: "user-b"}, nil
			}
			newEpoch := connect(t, s)
			checkToken := func(token string) {
				calls++
				if token != "account-b" {
					t.Errorf("unexpected provider token %q", token)
				}
			}
			f.DetailFunc = func(_ context.Context, token, _, _ string) (model.Item, error) {
				checkToken(token)
				return item(idA), nil
			}
			f.ListFunc = func(_ context.Context, token, _, _, _ string) (model.Page, error) {
				checkToken(token)
				return model.Page{Items: []model.Item{item(idA)}, Complete: true}, nil
			}
			read := func(epoch uint64) error {
				if operation == "detail" {
					_, err := s.Detail(context.Background(), epoch, "episode", idA)
					return err
				}
				view, err := s.Library(context.Background(), epoch, "favorites", "", "refresh")
				if err == nil && view.Error != nil {
					return view.Error
				}
				return err
			}
			if err := read(oldEpoch); !model.IsCode(err, "STALE_SESSION") || calls != 0 {
				t.Fatalf("old account read err=%v, provider calls=%d", err, calls)
			}
			if err := read(newEpoch); err != nil || calls != 1 {
				t.Fatalf("current account read err=%v, provider calls=%d", err, calls)
			}
		})
	}
}
