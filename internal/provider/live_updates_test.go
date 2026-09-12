package provider

import (
	"context"
	"os"
	"path/filepath"
	"starling/internal/security"
	"testing"
	"time"
)

// Explicit opt-in only. Reads the app's own protected session; never refreshes
// or saves credentials, publishes anything, or prints private episode metadata.
func TestLiveUpdatesReadOnly(t *testing.T) {
	if os.Getenv("STARLING_LIVE_UPDATES") != "1" {
		t.Skip("set STARLING_LIVE_UPDATES=1 for an authorized account read")
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
	cursor := ""
	ids := map[string]bool{}
	var previous time.Time
	orderInversions := 0
	for n := 1; n <= 2; n++ {
		p, e := c.List(ctx, saved.Credentials.Access, "updates", "", cursor)
		if e != nil {
			t.Fatal(e)
		}
		t.Logf("page=%d items=%d complete=%v cursor=%v", n, len(p.Items), p.Complete, p.Cursor != "")
		for _, it := range p.Items {
			if ids[it.ID] {
				t.Fatal("overlapping live pages; reconcile concurrent subscription changes")
			}
			ids[it.ID] = true
			if it.Kind != "episode" || it.PodcastID == "" || it.PodcastTitle == "" {
				t.Fatal("inbox item metadata incomplete")
			}
			date, e := time.Parse(time.RFC3339Nano, it.Published)
			if e != nil {
				t.Fatal("inbox date missing or invalid")
			}
			if !previous.IsZero() && date.After(previous) {
				orderInversions++
			}
			previous = date
		}
		if p.Complete {
			t.Logf("observed publication-order inversions=%d; UI sorts loaded metadata", orderInversions)
			return
		}
		if p.Cursor == "" {
			t.Logf("observed publication-order inversions=%d; UI sorts loaded metadata", orderInversions)
			t.Log("terminal semantics unconfirmed: no cursor or explicit end flag")
			return
		}
		if p.Cursor == cursor {
			t.Fatal("repeated cursor")
		}
		cursor = p.Cursor
	}
	t.Logf("observed publication-order inversions=%d; UI sorts loaded metadata", orderInversions)
	t.Log("two-page sample verified; full history and terminal page not checked")
}
