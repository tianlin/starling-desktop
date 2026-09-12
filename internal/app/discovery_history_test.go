package app

import (
	"context"
	"encoding/json"
	"fmt"
	"starling/internal/testkit"
	"strings"
	"testing"
)

func historyCall(t *testing.T, s *Service, epoch uint64, mode, query string) []string {
	t.Helper()
	args, _ := json.Marshal(map[string]any{"epoch": epoch, "mode": mode, "query": query})
	raw := s.Dispatch(context.Background(), "discovery.history", string(args))
	var r struct {
		OK   bool `json:"ok"`
		Data struct {
			Terms []string `json:"terms"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &r) != nil || !r.OK {
		t.Fatalf("history operation failed: %s", raw)
	}
	return r.Data.Terms
}

func TestDiscoveryHistoryDedupLimitDeleteAndClear(t *testing.T) {
	s := fixture(t, &testkit.Fake{})
	epoch := connect(t, s)
	for i := 0; i < 12; i++ {
		historyCall(t, s, epoch, "record", fmt.Sprintf("搜索%d", i))
	}
	got := historyCall(t, s, epoch, "record", "  搜索5  ")
	if len(got) != 10 || got[0] != "搜索5" || got[1] != "搜索11" {
		t.Fatal(got)
	}
	got = historyCall(t, s, epoch, "remove", "搜索5")
	if len(got) != 9 || got[0] != "搜索11" {
		t.Fatal(got)
	}
	got = historyCall(t, s, epoch, "clear", "")
	if len(got) != 0 {
		t.Fatal(got)
	}
}

func TestDiscoveryHistoryAccountIsolationAndLogoutCleanup(t *testing.T) {
	s := fixture(t, &testkit.Fake{})
	guest := s.Session().Epoch
	historyCall(t, s, guest, "record", "访客的词")
	epoch := connect(t, s)
	if got := historyCall(t, s, epoch, "read", ""); len(got) != 0 {
		t.Fatal(got)
	}
	historyCall(t, s, epoch, "record", "账号的词")
	if e := s.Logout(); e != nil {
		t.Fatal(e)
	}
	got := historyCall(t, s, s.Session().Epoch, "read", "")
	if len(got) != 1 || got[0] != "访客的词" {
		t.Fatal(got)
	}
	args := fmt.Sprintf(`{"epoch":%d,"mode":"record","query":"迟到的词"}`, epoch)
	raw := s.Dispatch(context.Background(), "discovery.history", args)
	if !strings.Contains(raw, "STALE_SESSION") {
		t.Fatal(raw)
	}
	var saved any
	if ok, e := s.store.Get("xiaoyuzhou:user-a", "discovery", "history", &saved); e != nil || ok {
		t.Fatal(saved, e)
	}
}

func TestDiscoveryHistoryRejectsInvalidQueriesWithoutRecording(t *testing.T) {
	s := fixture(t, &testkit.Fake{})
	epoch := s.Session().Epoch
	for _, query := range []string{" ", "bad\nquery", strings.Repeat("词", 201)} {
		args, _ := json.Marshal(map[string]any{"epoch": epoch, "mode": "record", "query": query})
		raw := s.Dispatch(context.Background(), "discovery.history", string(args))
		if !strings.Contains(raw, "INVALID_REQUEST") {
			t.Fatal(raw)
		}
	}
	if got := historyCall(t, s, epoch, "read", ""); len(got) != 0 {
		t.Fatal(got)
	}
	// Diagnostic records only action/code, not a user's search term.
	args, _ := json.Marshal(map[string]any{"epoch": epoch, "mode": "record", "query": "private-query\n"})
	s.Dispatch(context.Background(), "discovery.history", string(args))
	if b, _ := json.Marshal(s.log); strings.Contains(string(b), "private-query") {
		t.Fatal(string(b))
	}
}
