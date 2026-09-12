package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"starling/internal/model"
	"starling/internal/security"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in only: reads the app-owned protected vault, never refreshes or
// saves credentials and never calls Subscribe. Output contains only counts,
// envelope fields and normalized status; no queries, IDs or returned content.
func TestLiveDiscoveryReadOnly(t *testing.T) {
	if os.Getenv("STARLING_LIVE_DISCOVERY") != "1" {
		t.Skip("set STARLING_LIVE_DISCOVERY=1 for an explicitly authorized read-only account check")
	}
	base, e := os.UserConfigDir()
	if e != nil {
		t.Fatal(e)
	}
	saved, e := security.NewVault(filepath.Join(base, "Starling", "session.vault")).Load()
	if e != nil {
		t.Fatal(e)
	}
	if saved.Credentials.Access == "" {
		t.Fatal("no saved Starling session")
	}
	c := New()
	c.http.Transport = liveMetadataTransport{c.http.Transport, t}
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	suggestions, e := c.Suggestions(ctx, saved.Credentials.Access)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("suggestions count=%d", len(suggestions))
	query := os.Getenv("STARLING_LIVE_DISCOVERY_QUERY")
	if query == "" {
		query = "科技"
	}
	uid, pid := "", ""
	for _, kind := range []model.SearchKind{model.SearchPodcast, model.SearchEpisode, model.SearchUser} {
		p, e := c.Search(ctx, saved.Credentials.Access, query, kind, "")
		if e != nil {
			// Failure diagnostics expose structure only, never account content.
			b, _, err := c.request(ctx, "POST", c.api+"/v1/search/create", map[string]any{"limit": "20", "sourcePageName": "4", "currentPageName": "4", "type": strings.ToUpper(string(kind)), "keyword": query}, accessHeader(saved.Credentials.Access), true)
			if err == nil {
				var env map[string]json.RawMessage
				_ = json.Unmarshal(b, &env)
				keys := func(raw json.RawMessage) []string {
					var m map[string]json.RawMessage
					_ = json.Unmarshal(raw, &m)
					out := []string{}
					for k := range m {
						out = append(out, k)
					}
					sort.Strings(out)
					return out
				}
				var rows []json.RawMessage
				_ = json.Unmarshal(env["data"], &rows)
				t.Logf("search structure kind=%s keyFields=%v validKnownKey=%v rows=%d", kind, keys(env["loadMoreKey"]), validSearchKey(env["loadMoreKey"]), len(rows))
				if len(rows) > 0 {
					t.Logf("first result fields=%v", keys(rows[0]))
				}
			}
			t.Fatalf("search kind=%s: %v", kind, e)
		}
		t.Logf("search kind=%s items=%d users=%d complete=%v cursor=%v", kind, len(p.Items), len(p.Users), p.Complete, p.Cursor != "")
		if kind == model.SearchPodcast && len(p.Items) > 0 {
			pid = p.Items[0].ID
		}
		if len(p.Users) > 0 {
			uid = p.Users[0].ID
		}
		if p.Cursor != "" {
			next, e := c.Search(ctx, saved.Credentials.Access, query, kind, p.Cursor)
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("search second page kind=%s items=%d complete=%v cursor=%v", kind, len(next.Items), next.Complete, next.Cursor != "")
		}
	}
	if uid != "" {
		p, e := c.Creator(ctx, saved.Credentials.Access, uid, "")
		if e != nil {
			t.Fatal(e)
		}
		t.Logf("creator owned count=%d complete=%v", len(p.Items), p.Complete)
	}
	if pid != "" {
		p, e := c.SubscriptionState(ctx, saved.Credentials.Access, pid)
		if e != nil {
			t.Fatal(e)
		}
		t.Logf("subscription read status=%s", p.State)
	}
}
