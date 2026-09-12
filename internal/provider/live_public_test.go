package provider

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"starling/internal/security"
	"testing"
	"time"
)

// Explicit opt-in; uses only anonymous public pages. No vault, account API,
// media download, playback, or credential refresh is involved.
func TestLivePublicShare(t *testing.T) {
	raw := os.Getenv("STARLING_LIVE_PUBLIC_URL")
	if raw == "" {
		t.Skip("set STARLING_LIVE_PUBLIC_URL to an authorized public share URL")
	}
	share, err := security.ParseShare(raw)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	c := New()
	item, err := c.Detail(ctx, "", share.Kind, share.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != share.ID || item.Title == "" {
		t.Fatal("public detail identity/title missing")
	}
	t.Logf("kind=%s titlePresent=true mediaPresent=%v restricted=%v", share.Kind, item.MediaURL != "", item.Restricted)
	pid := item.PodcastID
	if share.Kind == "podcast" {
		pid = item.ID
	}
	if pid == "" {
		t.Fatal("public episode did not identify its podcast")
	}
	podcast, err := c.Detail(ctx, "", "podcast", pid)
	if err != nil {
		t.Fatal(err)
	}
	if podcast.ID != pid || podcast.Title == "" {
		t.Fatal("podcast identity/title missing")
	}
	data, schemaErr := c.publicData(ctx, "podcast", pid)
	if schemaErr != nil {
		t.Fatal(schemaErr)
	}
	keys := []string{}
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	t.Logf("podcast page keys=%v", keys)
	var nested map[string]json.RawMessage
	if json.Unmarshal(data["podcast"], &nested) == nil {
		keys = keys[:0]
		for key := range nested {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		t.Logf("podcast object keys=%v", keys)
	}
	page, err := c.List(ctx, "", "episodes", pid, "")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, episode := range page.Items {
		if episode.ID == "" || seen[episode.ID] {
			t.Fatal("missing or duplicate public episode ID")
		}
		seen[episode.ID] = true
	}
	if len(page.Items) == 0 {
		t.Fatal("expected public episodes for the selected podcast")
	}
	if page.Complete {
		t.Fatal("public first-page preview must not claim complete pagination")
	}
	t.Logf("podcastVerified=true publicPreviewItems=%d complete=%v", len(page.Items), page.Complete)
}
