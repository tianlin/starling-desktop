package app

import (
	"context"
	"fmt"
	"starling/internal/model"
	"starling/internal/testkit"
	"testing"
)

func TestUpdatesSnapshotAndFailurePreservation(t *testing.T) {
	fail := false
	f := &testkit.Fake{ListFunc: func(_ context.Context, _, kind, _, cursor string) (model.Page, error) {
		if kind != "updates" {
			t.Fatal(kind)
		}
		if fail {
			return model.Page{}, model.Err("NETWORK", "failed")
		}
		items := []model.Item{}
		for i := 0; i < 120; i++ {
			n := i
			if cursor != "" {
				n += 120
			}
			it := item(fmt.Sprintf("%024x", n+1))
			it.Published = fmt.Sprintf("2026-09-%02dT00:00:00Z", 1+n%10)
			it.MediaURL = "https://secret"
			items = append(items, it)
		}
		return model.Page{Items: items, Cursor: cursor + "a"}, nil
	}}
	s := fixture(t, f)
	epoch := connect(t, s)
	v, e := s.Library(context.Background(), epoch, "updates", "", "refresh")
	if e != nil || len(v.Items) != 120 {
		t.Fatal(v, e)
	}
	var cache model.LibraryView
	ok, e := s.store.Get("xiaoyuzhou:user-a", "cache", "updates:", &cache)
	if e != nil || !ok || len(cache.Items) != 120 || cache.Cursor != "" {
		t.Fatal(cache, e)
	}
	v, e = s.Library(context.Background(), epoch, "updates", "", "more")
	if e != nil || len(v.Items) != 240 {
		t.Fatal(v, e)
	}
	s.store.Get("xiaoyuzhou:user-a", "cache", "updates:", &cache)
	if len(cache.Items) != 200 || cache.Complete || cache.Status != "cached" {
		t.Fatal(cache)
	}
	at := v.UpdatedAt
	fail = true
	v, e = s.Library(context.Background(), epoch, "updates", "", "refresh")
	if e != nil || len(v.Items) != 240 || v.UpdatedAt != at || v.Error == nil {
		t.Fatal(v, e)
	}
	v, e = s.Library(context.Background(), epoch, "updates", "", "more")
	if e != nil || len(v.Items) != 240 || v.UpdatedAt != at || v.Error == nil {
		t.Fatal(v, e)
	}
	fail = false
	v, e = s.Library(context.Background(), epoch, "updates", "", "more")
	if e != nil || v.Error != nil {
		t.Fatal(v, e)
	}
	s.mu.Lock()
	s.stages = map[string]*libraryStage{}
	s.mu.Unlock()
	v, e = s.Library(context.Background(), epoch, "updates", "", "cached")
	if e != nil || len(v.Items) != 200 || v.Cursor != "" || v.Complete {
		t.Fatal(v, e)
	}
	_, e = s.Library(context.Background(), epoch, "updates", "", "more")
	if !model.IsCode(e, "PAGINATION") {
		t.Fatal(e)
	}
}

func TestUpdatesInvalidPageIsAtomic(t *testing.T) {
	bad := false
	s := fixture(t, &testkit.Fake{ListFunc: func(context.Context, string, string, string, string) (model.Page, error) {
		if bad {
			return model.Page{Items: []model.Item{item(idB), item("bad")}, Cursor: "b"}, nil
		}
		return model.Page{Items: []model.Item{item(idA)}, Cursor: "a"}, nil
	}})
	epoch := connect(t, s)
	before, e := s.Library(context.Background(), epoch, "updates", "", "refresh")
	if e != nil {
		t.Fatal(e)
	}
	bad = true
	v, e := s.Library(context.Background(), epoch, "updates", "", "more")
	if e != nil || v.Error == nil || len(v.Items) != 1 || v.Items[0].ID != idA || v.Cursor != before.Cursor || v.UpdatedAt != before.UpdatedAt {
		t.Fatal(v, e)
	}
}

func TestUpdatesPaginationGuards(t *testing.T) {
	for _, tc := range []struct {
		name string
		page model.Page
	}{
		{"repeat", model.Page{Items: []model.Item{item(idB)}, Cursor: "a"}},
		{"empty_cursor", model.Page{Cursor: "b"}},
		{"conflict", model.Page{Items: []model.Item{item(idB)}, Cursor: "b", Complete: true}},
		{"wrong_kind", model.Page{Items: []model.Item{{ID: idB, Kind: "podcast"}}, Cursor: "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t, &testkit.Fake{ListFunc: func(_ context.Context, _, _, _, cur string) (model.Page, error) {
				if cur == "" {
					return model.Page{Items: []model.Item{item(idA)}, Cursor: "a"}, nil
				}
				return tc.page, nil
			}})
			epoch := connect(t, s)
			before, _ := s.Library(context.Background(), epoch, "updates", "", "refresh")
			v, e := s.Library(context.Background(), epoch, "updates", "", "more")
			if e != nil || v.Error == nil || len(v.Items) != 1 || v.Cursor != "a" || v.UpdatedAt != before.UpdatedAt {
				t.Fatal(v, e)
			}
		})
	}
}

func TestUpdatesLateResultIsolation(t *testing.T) {
	for _, action := range []string{"cancel", "clear", "logout", "reset"} {
		t.Run(action, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			s := fixture(t, &testkit.Fake{ListFunc: func(context.Context, string, string, string, string) (model.Page, error) {
				close(started)
				<-release
				return model.Page{Items: []model.Item{item(idA)}, Complete: true}, nil
			}})
			epoch := connect(t, s)
			done := make(chan error, 1)
			go func() { _, e := s.Library(context.Background(), epoch, "updates", "", "refresh"); done <- e }()
			<-started
			var e error
			switch action {
			case "cancel":
				_, e = s.Library(context.Background(), epoch, "updates", "", "cancel")
			case "clear":
				e = s.ClearCache(epoch)
			case "logout":
				e = s.Logout()
			case "reset":
				e = s.Reset(epoch, "RESET")
			}
			if e != nil {
				t.Fatal(e)
			}
			close(release)
			if e = <-done; !model.IsCode(e, "STALE_SESSION") {
				t.Fatal(e)
			}
			var cached model.LibraryView
			ok, e := s.store.Get("xiaoyuzhou:user-a", "cache", "updates:", &cached)
			if e != nil || ok {
				t.Fatal(cached, e)
			}
		})
	}
}

func TestUpdatesOrderingAndReplacement(t *testing.T) {
	phase := 0
	s := fixture(t, &testkit.Fake{ListFunc: func(_ context.Context, _, _, _, cur string) (model.Page, error) {
		if phase == 1 {
			return model.Page{Items: []model.Item{item(idB)}}, nil
		}
		items := []model.Item{}
		if cur == "" {
			for i := 0; i < 205; i++ {
				it := item(fmt.Sprintf("%024x", i+1))
				it.Published = "2026-09-01T00:00:00Z"
				items = append(items, it)
			}
			return model.Page{Items: items, Cursor: "next"}, nil
		}
		newer := item(idA)
		newer.Published = "2026-09-02T00:00:00Z"
		newer.MediaURL = "secret"
		newer.ShowNotes = "private"
		newer.Image = "https://evil.example/a"
		newer.SourceURL = "https://evil.example/media"
		return model.Page{Items: []model.Item{item(idB), newer}, Complete: true}, nil
	}})
	epoch := connect(t, s)
	s.Library(context.Background(), epoch, "updates", "", "refresh")
	v, e := s.Library(context.Background(), epoch, "updates", "", "more")
	if e != nil || !v.Complete || len(v.Items) != 207 || v.Items[0].ID != idA || v.Items[206].ID != idB {
		t.Fatal(v, e)
	}
	var cached model.LibraryView
	s.store.Get("xiaoyuzhou:user-a", "cache", "updates:", &cached)
	if len(cached.Items) != 200 || cached.Complete || cached.Items[0].ID != idA || cached.Items[1].ID != fmt.Sprintf("%024x", 1) {
		t.Fatal(cached)
	}
	if v.Items[0].MediaURL != "" || v.Items[0].ShowNotes != "" || v.Items[0].Image != "" || v.Items[0].SourceURL != item(idA).SourceURL {
		t.Fatal(v.Items[0])
	}
	phase = 1
	v, e = s.Library(context.Background(), epoch, "updates", "", "refresh")
	if e != nil || len(v.Items) != 1 || v.Items[0].ID != idB || v.Status != "unknown_end" || v.Complete {
		t.Fatal(v, e)
	}
	s.store.Get("xiaoyuzhou:user-a", "cache", "updates:", &cached)
	if len(cached.Items) != 1 || cached.Items[0].ID != idB {
		t.Fatal(cached)
	}
}

func TestUpdatesPrivateAndStale(t *testing.T) {
	s := fixture(t, &testkit.Fake{})
	_, e := s.Library(context.Background(), s.Session().Epoch, "updates", "", "cached")
	if !model.IsCode(e, "UNAUTHORIZED") {
		t.Fatal(e)
	}
	epoch := connect(t, s)
	s.Logout()
	connect(t, s)
	_, e = s.Library(context.Background(), epoch, "updates", "", "cached")
	if !model.IsCode(e, "STALE_SESSION") {
		t.Fatal(e)
	}
}

func TestUpdatesAccountCacheIsolation(t *testing.T) {
	user := "user-a"
	f := &testkit.Fake{
		MeFunc: func(context.Context, string) (model.Identity, error) { return model.Identity{ID: user}, nil },
		LoginFunc: func(context.Context, string, string, string) (model.Credentials, model.Identity, error) {
			return model.Credentials{Access: "access", Refresh: "refresh"}, model.Identity{ID: user}, nil
		},
		ListFunc: func(context.Context, string, string, string, string) (model.Page, error) {
			return model.Page{Items: []model.Item{item(idA)}, Complete: true}, nil
		},
	}
	s := fixture(t, f)
	epoch := connect(t, s)
	if _, e := s.Library(context.Background(), epoch, "updates", "", "refresh"); e != nil {
		t.Fatal(e)
	}
	if e := s.Logout(); e != nil {
		t.Fatal(e)
	}
	user = "user-b"
	epoch = connect(t, s)
	v, e := s.Library(context.Background(), epoch, "updates", "", "cached")
	if e != nil || len(v.Items) != 0 || v.Status != "idle" {
		t.Fatal(v, e)
	}
}

func TestUpdatesUnknownEmptyAndBounds(t *testing.T) {
	s := fixture(t, &testkit.Fake{ListFunc: func(context.Context, string, string, string, string) (model.Page, error) {
		return model.Page{Items: []model.Item{}}, nil
	}})
	epoch := connect(t, s)
	v, e := s.Library(context.Background(), epoch, "updates", "", "refresh")
	if e != nil || v.Complete || v.Status != "unknown_end" {
		t.Fatal(v, e)
	}
	s.mu.Lock()
	for _, stage := range s.stages {
		stage.view.Pages = 1000
		stage.view.Cursor = "next"
	}
	s.mu.Unlock()
	_, e = s.Library(context.Background(), epoch, "updates", "", "more")
	if !model.IsCode(e, "PAGINATION") {
		t.Fatal(e)
	}
}

func TestUpdatesDiskFailurePreservesStage(t *testing.T) {
	s := fixture(t, &testkit.Fake{ListFunc: func(_ context.Context, _, _, _, cur string) (model.Page, error) {
		if cur == "" {
			return model.Page{Items: []model.Item{item(idA)}, Cursor: "a"}, nil
		}
		return model.Page{Items: []model.Item{item(idB)}, Complete: true}, nil
	}})
	epoch := connect(t, s)
	before, e := s.Library(context.Background(), epoch, "updates", "", "refresh")
	if e != nil {
		t.Fatal(e)
	}
	s.store.Close()
	v, e := s.Library(context.Background(), epoch, "updates", "", "more")
	if e != nil || v.Error == nil || len(v.Items) != 1 || v.Cursor != before.Cursor || v.UpdatedAt != before.UpdatedAt {
		t.Fatal(v, e)
	}
}
