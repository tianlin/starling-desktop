package app

import (
	"context"
	"encoding/json"
	"fmt"
	"starling/internal/model"
	"starling/internal/testkit"
	"sync/atomic"
	"testing"
	"time"
)

func TestLibraryPaginationDedup(t *testing.T) {
	var count atomic.Int32
	f := &testkit.Fake{ListFunc: func(c context.Context, tok, k, p, cur string) (model.Page, error) {
		count.Add(1)
		if cur == "" {
			return model.Page{Items: []model.Item{item(idA)}, Cursor: "cursor-a"}, nil
		}
		return model.Page{Items: []model.Item{item(idA), item(idB)}, Complete: true}, nil
	}}
	s := fixture(t, f)
	epoch := connect(t, s)
	v, e := s.Library(context.Background(), epoch, "favorites", "", "refresh")
	if e != nil || v.Complete || len(v.Items) != 1 {
		t.Fatal(v, e)
	}
	v, e = s.Library(context.Background(), epoch, "favorites", "", "more")
	if e != nil || !v.Complete || len(v.Items) != 2 || count.Load() != 2 {
		t.Fatal(v, e)
	}
	var cached model.LibraryView
	ok, e := s.store.Get("xiaoyuzhou:user-a", "cache", "favorites:", &cached)
	if e != nil || !ok || len(cached.Items) != 2 {
		t.Fatal(cached, e)
	}
}
func TestRepeatedCursorPreservesLastComplete(t *testing.T) {
	var phase atomic.Int32
	f := &testkit.Fake{ListFunc: func(c context.Context, tok, k, p, cur string) (model.Page, error) {
		if phase.Load() == 0 {
			return model.Page{Items: []model.Item{item(idA), item(idB)}, Complete: true}, nil
		}
		return model.Page{Items: []model.Item{item(idA)}, Cursor: "repeat"}, nil
	}}
	s := fixture(t, f)
	epoch := connect(t, s)
	s.Library(context.Background(), epoch, "favorites", "", "refresh")
	phase.Store(1)
	s.Library(context.Background(), epoch, "favorites", "", "refresh")
	v, e := s.Library(context.Background(), epoch, "favorites", "", "more")
	if e != nil || v.Status != "error" || v.Error == nil || v.Error.Code != "PAGINATION" {
		t.Fatal(v, e)
	}
	var cached model.LibraryView
	s.store.Get("xiaoyuzhou:user-a", "cache", "favorites:", &cached)
	if len(cached.Items) != 2 {
		t.Fatal("partial replaced complete snapshot")
	}
}
func TestPrivateLibraryNeverGuestEmpty(t *testing.T) {
	s := fixture(t, &testkit.Fake{})
	_, e := s.Library(context.Background(), s.session.View().Epoch, "favorites", "", "refresh")
	if !model.IsCode(e, "UNAUTHORIZED") {
		t.Fatal(e)
	}
}
func TestCancelledRefreshCannotRepopulate(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	f := &testkit.Fake{ListFunc: func(context.Context, string, string, string, string) (model.Page, error) {
		close(started)
		<-release
		return model.Page{Items: []model.Item{item(idA)}, Complete: true}, nil
	}}
	s := fixture(t, f)
	epoch := connect(t, s)
	done := make(chan error, 1)
	go func() { _, e := s.Library(context.Background(), epoch, "favorites", "", "refresh"); done <- e }()
	<-started
	s.Logout()
	close(release)
	if e := <-done; !model.IsCode(e, "STALE_SESSION") {
		t.Fatal(e)
	}
}
func TestUnknownEndNotComplete(t *testing.T) {
	f := &testkit.Fake{ListFunc: func(context.Context, string, string, string, string) (model.Page, error) {
		return model.Page{Items: []model.Item{item(idA)}}, nil
	}}
	s := fixture(t, f)
	epoch := connect(t, s)
	v, e := s.Library(context.Background(), epoch, "favorites", "", "refresh")
	if e != nil || v.Complete || v.Status != "unknown_end" {
		t.Fatal(v, e)
	}
}

func TestLibraryFailureIncludedInDiagnosticsWithCachePreserved(t *testing.T) {
	failed := false
	f := &testkit.Fake{ListFunc: func(context.Context, string, string, string, string) (model.Page, error) {
		if failed {
			return model.Page{}, model.Err("REQUEST_REJECTED", "fixture failure")
		}
		return model.Page{Items: []model.Item{item(idA)}, Complete: true}, nil
	}}
	s := fixture(t, f)
	epoch := connect(t, s)
	args := fmt.Sprintf(`{"epoch":%d,"kind":"favorites","mode":"refresh"}`, epoch)
	s.Dispatch(context.Background(), "library", args)
	failed = true
	var result struct {
		OK   bool              `json:"ok"`
		Data model.LibraryView `json:"data"`
	}
	if e := json.Unmarshal([]byte(s.Dispatch(context.Background(), "library", args)), &result); e != nil {
		t.Fatal(e)
	}
	if !result.OK || result.Data.Error == nil || len(result.Data.Items) != 1 {
		t.Fatal("library error must preserve cached view")
	}
	var report struct {
		Data struct {
			Events []diagnosticEvent `json:"events"`
		} `json:"data"`
	}
	json.Unmarshal([]byte(s.Dispatch(context.Background(), "diagnostics", `{}`)), &report)
	if len(report.Data.Events) != 1 || report.Data.Events[0].Action != "library" || report.Data.Events[0].Code != "REQUEST_REJECTED" {
		t.Fatalf("missing nested library error: %+v", report.Data.Events)
	}
}

func TestInMemoryCompletedLibraryExpiresWhenRevisited(t *testing.T) {
	s := fixture(t, &testkit.Fake{ListFunc: func(context.Context, string, string, string, string) (model.Page, error) {
		return model.Page{Items: []model.Item{item(idA)}, Complete: true}, nil
	}})
	epoch := connect(t, s)
	s.Library(context.Background(), epoch, "favorites", "", "refresh")
	v, e := s.Library(context.Background(), epoch, "favorites", "", "cached")
	if e != nil || v.Status != "cached" {
		t.Fatalf("fresh cached status=%s err=%v", v.Status, e)
	}
	s.mu.Lock()
	for _, stage := range s.stages {
		stage.view.UpdatedAt = time.Now().Add(-11 * time.Minute).Format(time.RFC3339Nano)
	}
	s.mu.Unlock()
	v, e = s.Library(context.Background(), epoch, "favorites", "", "cached")
	if e != nil || v.Status != "stale" || len(v.Items) != 1 {
		t.Fatalf("expired cache status=%s err=%v", v.Status, e)
	}
}
