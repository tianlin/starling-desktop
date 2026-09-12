package app

import (
	"context"
	"path/filepath"
	"starling/internal/model"
	"starling/internal/store"
	"starling/internal/testkit"
	"strings"
	"sync/atomic"
	"testing"
)

type discoveryFake struct {
	*testkit.Fake
	search    func(context.Context, string, string, model.SearchKind, string) (model.SearchPage, error)
	subscribe func(context.Context, string, string) (model.SubscriptionResult, error)
	status    func(context.Context, string, string) (model.SubscriptionResult, error)
}

func (f *discoveryFake) Search(c context.Context, t, q string, k model.SearchKind, cur string) (model.SearchPage, error) {
	if f.search != nil {
		return f.search(c, t, q, k, cur)
	}
	return model.SearchPage{Items: []model.Item{}, Users: []model.Creator{}, Complete: true}, nil
}
func (f *discoveryFake) Suggestions(context.Context, string) ([]string, error) {
	return []string{"科学", "科学", "生活"}, nil
}
func (f *discoveryFake) Creator(context.Context, string, string, string) (model.CreatorPage, error) {
	return model.CreatorPage{Creator: model.Creator{ID: idA, Nickname: "创作者"}, Items: []model.Item{}, Complete: true}, nil
}
func (f *discoveryFake) Subscribe(c context.Context, t, p string) (model.SubscriptionResult, error) {
	if f.subscribe != nil {
		return f.subscribe(c, t, p)
	}
	it := podcastItem(p)
	return model.SubscriptionResult{PodcastID: p, State: model.SubscriptionOn, Item: &it}, nil
}
func (f *discoveryFake) SubscriptionState(c context.Context, t, p string) (model.SubscriptionResult, error) {
	if f.status != nil {
		return f.status(c, t, p)
	}
	return model.SubscriptionResult{PodcastID: p, State: model.SubscriptionUnknown}, nil
}
func discoveryFixture(t *testing.T, f *discoveryFake) *Service {
	t.Helper()
	if f.Fake == nil {
		f.Fake = &testkit.Fake{}
	}
	db, e := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if e != nil {
		t.Fatal(e)
	}
	s := New(f, db, &testkit.MemoryVault{})
	t.Cleanup(func() { s.Close(); db.Close() })
	return s
}
func podcastItem(id string) model.Item {
	return model.Item{Kind: "podcast", ID: id, Title: "探索节目", SourceURL: "https://www.xiaoyuzhoufm.com/podcast/" + id}
}

func TestDiscoverySearchRejectsLateGenerationAndAccountChange(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	f := &discoveryFake{search: func(c context.Context, t, q string, k model.SearchKind, cur string) (model.SearchPage, error) {
		if q == "旧搜索" {
			close(started)
			<-release
		}
		return model.SearchPage{Items: []model.Item{podcastItem(idA)}, Complete: true}, nil
	}}
	s := discoveryFixture(t, f)
	epoch := connect(t, s)
	done := make(chan error, 1)
	go func() {
		_, e := s.DiscoverySearch(context.Background(), epoch, 1, "旧搜索", model.SearchPodcast, "")
		done <- e
	}()
	<-started
	if _, e := s.DiscoverySearch(context.Background(), epoch, 2, "新搜索", model.SearchPodcast, ""); e != nil {
		t.Fatal(e)
	}
	close(release)
	if e := <-done; !model.IsCode(e, "STALE_SESSION") {
		t.Fatal(e)
	}
	s.Logout()
	if _, e := s.DiscoverySearch(context.Background(), epoch, 3, "搜索", model.SearchPodcast, ""); !model.IsCode(e, "STALE_SESSION") {
		t.Fatal(e)
	}
}

func TestDiscoverySearchSanitizesAndDoesNotPersistResults(t *testing.T) {
	f := &discoveryFake{search: func(c context.Context, t, q string, k model.SearchKind, cur string) (model.SearchPage, error) {
		it := podcastItem(idA)
		it.MediaURL = "https://media.xyzcdn.net/private-signed"
		it.Image = "http://127.0.0.1/x"
		return model.SearchPage{Items: []model.Item{it, it}, Subscriptions: map[string]model.SubscriptionState{idA: model.SubscriptionUnknown}, Complete: true}, nil
	}}
	s := discoveryFixture(t, f)
	epoch := connect(t, s)
	p, e := s.DiscoverySearch(context.Background(), epoch, 1, "科学", model.SearchPodcast, "")
	if e != nil || len(p.Items) != 1 || p.Items[0].Image != "" || p.Items[0].MediaURL != "" || p.Subscriptions[idA] != model.SubscriptionUnknown {
		t.Fatal(p, e)
	}
	rows, e := s.store.List("xiaoyuzhou:user-a", "cache")
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	v, e := s.DiscoverySuggestions(context.Background(), epoch)
	if e != nil || len(v.Terms) != 2 {
		t.Fatal(v, e)
	}
	creator, e := s.DiscoveryCreator(context.Background(), epoch, idA, "")
	if e != nil || len(creator.Items) != 0 || !creator.Complete {
		t.Fatal(creator, e)
	}
}

func TestSubscribeOnceDedupAndConfirmedLibraryOverlay(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	f := &discoveryFake{subscribe: func(c context.Context, t, p string) (model.SubscriptionResult, error) {
		calls.Add(1)
		close(started)
		<-release
		it := podcastItem(p)
		return model.SubscriptionResult{PodcastID: p, State: model.SubscriptionOn, Item: &it}, nil
	}}
	s := discoveryFixture(t, f)
	epoch := connect(t, s)
	done := make(chan error, 1)
	go func() { _, e := s.AddSubscription(context.Background(), epoch, idA, "request-first"); done <- e }()
	<-started
	if _, e := s.AddSubscription(context.Background(), epoch, idA, "request-other"); !model.IsCode(e, "SUBSCRIPTION_BUSY") {
		t.Fatal(e)
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if _, e := s.AddSubscription(context.Background(), epoch, idA, "request-first"); !model.IsCode(e, "SUBSCRIPTION_DUPLICATE") {
		t.Fatal(e)
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	v, e := s.Library(context.Background(), epoch, "subscriptions", "", "cached")
	if e != nil || len(v.Items) != 1 || v.Items[0].ID != idA {
		t.Fatal(v, e)
	}
	// An empty, eventually consistent full refresh must not erase a just-confirmed add.
	v, e = s.Library(context.Background(), epoch, "subscriptions", "", "refresh")
	if e != nil || len(v.Items) != 1 || v.Items[0].ID != idA {
		t.Fatal(v, e)
	}
}

func TestSubscriptionUncertainReadbackDoesNotReplay(t *testing.T) {
	var writes atomic.Int32
	var reads atomic.Int32
	f := &discoveryFake{subscribe: func(context.Context, string, string) (model.SubscriptionResult, error) {
		writes.Add(1)
		return model.SubscriptionResult{}, model.Err("SUBSCRIPTION_UNCERTAIN", "结果待确认")
	}, status: func(c context.Context, t, p string) (model.SubscriptionResult, error) {
		reads.Add(1)
		it := podcastItem(p)
		return model.SubscriptionResult{PodcastID: p, State: model.SubscriptionOn, Item: &it}, nil
	}}
	s := discoveryFixture(t, f)
	epoch := connect(t, s)
	v, e := s.AddSubscription(context.Background(), epoch, idA, "uncertain-request")
	if e != nil || v.State != model.SubscriptionOn || writes.Load() != 1 || reads.Load() != 1 {
		t.Fatal(v, e, writes.Load(), reads.Load())
	}
}

func TestSubscriptionUnconfirmedAndUnauthorizedNeverRefreshWrite(t *testing.T) {
	for _, code := range []string{"SUBSCRIPTION_UNCERTAIN", "UNAUTHORIZED"} {
		t.Run(code, func(t *testing.T) {
			var calls, refresh atomic.Int32
			f := &discoveryFake{Fake: &testkit.Fake{RefreshFunc: func(context.Context, model.Credentials) (model.Credentials, error) {
				refresh.Add(1)
				return model.Credentials{}, nil
			}}, subscribe: func(context.Context, string, string) (model.SubscriptionResult, error) {
				calls.Add(1)
				return model.SubscriptionResult{}, model.Err(code, "fixture")
			}}
			s := discoveryFixture(t, f)
			epoch := connect(t, s)
			_, e := s.AddSubscription(context.Background(), epoch, idA, "request-failed")
			if !model.IsCode(e, code) || calls.Load() != 1 || refresh.Load() != 0 {
				t.Fatal(e, calls.Load(), refresh.Load())
			}
			v, e := s.Library(context.Background(), epoch, "subscriptions", "", "cached")
			if code != "UNAUTHORIZED" && (e != nil || len(v.Items) != 0) {
				t.Fatal(v, e)
			}
		})
	}
}

func TestSubscriptionStalePageCannotEraseConfirmedAdd(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	f := &discoveryFake{Fake: &testkit.Fake{ListFunc: func(context.Context, string, string, string, string) (model.Page, error) {
		close(started)
		<-release
		return model.Page{Items: []model.Item{podcastItem(idB)}, Complete: true}, nil
	}}}
	s := discoveryFixture(t, f)
	epoch := connect(t, s)
	done := make(chan error, 1)
	go func() { _, e := s.Library(context.Background(), epoch, "subscriptions", "", "refresh"); done <- e }()
	<-started
	if _, e := s.AddSubscription(context.Background(), epoch, idA, "race-request"); e != nil {
		t.Fatal(e)
	}
	close(release)
	if e := <-done; !model.IsCode(e, "STALE_SESSION") {
		t.Fatal(e)
	}
	v, e := s.Library(context.Background(), epoch, "subscriptions", "", "cached")
	if e != nil || len(v.Items) != 1 || v.Items[0].ID != idA {
		t.Fatal(v, e)
	}
}

func TestDiscoveryBridgeStrictFieldsAndUnsupportedProvider(t *testing.T) {
	s := fixture(t, &testkit.Fake{})
	epoch := connect(t, s)
	raw := s.Dispatch(context.Background(), "discovery.search", `{"epoch":1,"query":"x","kind":"podcast","generation":1,"url":"evil"}`)
	if !strings.Contains(raw, "INVALID_REQUEST") {
		t.Fatal(raw)
	}
	if _, e := s.DiscoverySuggestions(context.Background(), epoch); !model.IsCode(e, "UNSUPPORTED") {
		t.Fatal(e)
	}
}

func TestDiscoveryBootstrapSeedsGenerationAfterPageReload(t *testing.T) {
	s := discoveryFixture(t, &discoveryFake{})
	epoch := connect(t, s)
	if _, e := s.DiscoverySearch(context.Background(), epoch, 41, "科学", model.SearchPodcast, ""); e != nil {
		t.Fatal(e)
	}
	b, e := s.Bootstrap()
	if e != nil || b.DiscoveryGeneration != 41 {
		t.Fatal(b.DiscoveryGeneration, e)
	}
	if _, e = s.DiscoverySearch(context.Background(), epoch, b.DiscoveryGeneration+1, "生活", model.SearchPodcast, ""); e != nil {
		t.Fatal(e)
	}
}

func TestSubscriptionLocalSaveFailureRemainsConfirmed(t *testing.T) {
	s := discoveryFixture(t, &discoveryFake{})
	epoch := connect(t, s)
	if e := s.store.Close(); e != nil {
		t.Fatal(e)
	}
	v, e := s.AddSubscription(context.Background(), epoch, idA, "local-cache-fail")
	if e != nil || v.State != model.SubscriptionOn || v.Warning == nil || v.Warning.Code != "SUBSCRIPTION_LOCAL_CACHE" {
		t.Fatal(v, e)
	}
	if s.subscriptionConfirmed[idA].ID != idA {
		t.Fatal("lost confirmed receipt")
	}
	for _, mode := range []string{"cached", "refresh"} {
		view, err := s.Library(context.Background(), epoch, "subscriptions", "", mode)
		if err != nil || len(view.Items) != 1 || view.Items[0].ID != idA || view.Error == nil {
			t.Fatal(mode, view, err)
		}
	}
}

func TestSubscriptionRefreshFailurePreservesOldCacheAndNewReceipt(t *testing.T) {
	var failure bool
	f := &discoveryFake{Fake: &testkit.Fake{ListFunc: func(context.Context, string, string, string, string) (model.Page, error) {
		if failure {
			return model.Page{}, model.Err("NETWORK", "offline")
		}
		return model.Page{Items: []model.Item{podcastItem(idB)}, Complete: true}, nil
	}}}
	s := discoveryFixture(t, f)
	epoch := connect(t, s)
	if _, e := s.Library(context.Background(), epoch, "subscriptions", "", "refresh"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.AddSubscription(context.Background(), epoch, idA, "preserve-cache"); e != nil {
		t.Fatal(e)
	}
	failure = true
	v, e := s.Library(context.Background(), epoch, "subscriptions", "", "refresh")
	if e != nil || len(v.Items) != 2 || v.Error == nil || v.Error.Code != "NETWORK" {
		t.Fatal(v, e)
	}
	var cache model.LibraryView
	_, e = s.store.Get("xiaoyuzhou:user-a", "cache", "subscriptions:", &cache)
	if e != nil || len(cache.Items) != 1 || cache.Items[0].ID != idB {
		t.Fatal(cache, e)
	}
}

func TestSubscriptionLogoutDropsLateSuccess(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	f := &discoveryFake{subscribe: func(c context.Context, t, p string) (model.SubscriptionResult, error) {
		close(started)
		<-release
		it := podcastItem(p)
		return model.SubscriptionResult{PodcastID: p, State: model.SubscriptionOn, Item: &it}, nil
	}}
	s := discoveryFixture(t, f)
	epoch := connect(t, s)
	done := make(chan error, 1)
	go func() { _, e := s.AddSubscription(context.Background(), epoch, idA, "logout-race"); done <- e }()
	<-started
	if e := s.Logout(); e != nil {
		t.Fatal(e)
	}
	close(release)
	if e := <-done; !model.IsCode(e, "STALE_SESSION") {
		t.Fatal(e)
	}
	if len(s.subscriptionConfirmed) != 0 {
		t.Fatal("old account receipt leaked")
	}
	rows, e := s.store.List("xiaoyuzhou:user-a", "subscription-confirmed")
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
}
