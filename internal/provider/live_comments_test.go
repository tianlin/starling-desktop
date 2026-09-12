package provider

import (
	"context"
	"os"
	"path/filepath"
	"starling/internal/security"
	"testing"
	"time"
)

// Explicit opt-in: app-owned vault only, no credential refresh/save and no writes.
// Logs only envelope field names, status and counts; never identifiers or content.
func TestLiveCommentsReadOnly(t *testing.T) {
	if os.Getenv("STARLING_LIVE_COMMENTS") != "1" {
		t.Skip("set STARLING_LIVE_COMMENTS=1 for an explicitly authorized read-only account check")
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	eid := os.Getenv("STARLING_LIVE_COMMENT_EPISODE")
	if eid == "" {
		p, e := c.List(ctx, saved.Credentials.Access, "favorites", "", "")
		if e != nil {
			t.Fatal(e)
		}
		if len(p.Items) == 0 {
			t.Skip("no favorite episode available; specify STARLING_LIVE_COMMENT_EPISODE")
		}
		eid = p.Items[0].ID
	}
	cursor := ""
	seen := map[string]bool{}
	threadReads := 0
	for n := 1; n <= 3; n++ {
		p, e := c.Comments(ctx, saved.Credentials.Access, eid, cursor)
		if e != nil {
			t.Fatal(e)
		}
		t.Logf("primary page=%d items=%d complete=%v cursor=%v", n, len(p.Items), p.Complete, p.Cursor != "")
		for _, item := range p.Items {
			if item.ReplyCount == 0 || threadReads >= 2 {
				continue
			}
			threadReads++
			replies, e := c.CommentThread(ctx, saved.Credentials.Access, eid, item.ID, "")
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("thread sample=%d expected=%d items=%d complete=%v cursor=%v", threadReads, item.ReplyCount, len(replies.Items), replies.Complete, replies.Cursor != "")
		}
		if p.Complete {
			break
		}
		if p.Cursor == "" || p.Cursor == cursor || seen[p.Cursor] {
			t.Fatal("missing or repeated primary cursor")
		}
		seen[p.Cursor] = true
		cursor = p.Cursor
	}
	if threadReads == 0 {
		t.Log("no replies in sampled primary pages; live thread contract unverified")
	}
}
