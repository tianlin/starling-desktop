package provider

import (
	"context"
	"os"
	"path/filepath"
	"starling/internal/security"
	"testing"
	"time"
)

// Explicit opt-in, read-only account probe. Logs counts only, never content or IDs.
func TestLiveCommentThreadsReadOnly(t *testing.T) {
	eid := os.Getenv("STARLING_LIVE_COMMENT_EPISODE")
	if os.Getenv("STARLING_LIVE_COMMENTS") != "1" || eid == "" {
		t.Skip("requires explicit live comments opt-in and an episode")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := security.NewVault(filepath.Join(base, "Starling", "session.vault")).Load()
	if err != nil {
		t.Fatal(err)
	}
	c := New()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, err := c.Comments(ctx, saved.Credentials.Access, eid, "")
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	for _, root := range page.Items {
		if root.ReplyCount == 0 || reads >= 10 {
			continue
		}
		reads++
		thread, err := c.CommentThread(ctx, saved.Credentials.Access, eid, root.ID, "")
		if err != nil {
			t.Fatalf("thread sample=%d: %v", reads, err)
		}
		removed := 0
		for _, reply := range thread.Items {
			if reply.ReplyTo != nil && reply.ReplyTo.Summary == "原评论已删除" {
				removed++
			}
		}
		t.Logf("thread sample=%d expected=%d returned=%d complete=%v removedReferences=%d", reads, root.ReplyCount, len(thread.Items), thread.Complete, removed)
	}
	if reads == 0 {
		t.Skip("no existing replies on the first HOT page")
	}
}
