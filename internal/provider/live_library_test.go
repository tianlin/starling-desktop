package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"starling/internal/security"
	"testing"
	"time"
)

type liveMetadataTransport struct {
	base http.RoundTripper
	t    *testing.T
}

func (l liveMetadataTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := l.base.RoundTrip(r)
	if err != nil {
		return resp, err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) == nil {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		l.t.Logf("response status=%d keys=%v", resp.StatusCode, keys)
	}
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	return resp, nil
}

// Manual opt-in only. Uses Starling's own vault, never refreshes/writes credentials,
// and emits counts/status only. Normal tests and CI never contact a real account.
func TestLiveLibraryReadOnly(t *testing.T) {
	if os.Getenv("STARLING_LIVE_LIBRARY") != "1" {
		t.Skip("set STARLING_LIVE_LIBRARY=1 for an explicitly authorized account check")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := security.NewVault(filepath.Join(base, "Starling", "session.vault")).Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Credentials.Access == "" {
		t.Fatal("no saved Starling session")
	}
	c := New()
	c.http.Transport = liveMetadataTransport{c.http.Transport, t}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, kind := range []string{"favorites", "subscriptions"} {
		t.Run(kind, func(t *testing.T) {
			cursor := ""
			seen := map[string]bool{}
			ids := map[string]bool{}
			for pageNo := 1; pageNo <= 100; pageNo++ {
				p, err := c.List(ctx, saved.Credentials.Access, kind, "", cursor)
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range p.Items {
					if ids[item.Key()] {
						t.Fatal("duplicate item in live snapshot; reconcile with mobile changes")
					}
					ids[item.Key()] = true
				}
				t.Logf("page=%d items=%d total=%d complete=%v cursor=%v", pageNo, len(p.Items), len(ids), p.Complete, p.Cursor != "")
				if p.Complete {
					return
				}
				if p.Cursor == "" {
					t.Fatal("unconfirmed pagination end")
				}
				if seen[p.Cursor] || p.Cursor == cursor {
					t.Fatal("repeated cursor")
				}
				seen[p.Cursor] = true
				cursor = p.Cursor
			}
			t.Fatal("manual check page limit reached")
		})
	}
}
