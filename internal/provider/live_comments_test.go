package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"starling/internal/model"
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
	threadReads := 0
	for _, order := range []model.CommentOrder{model.CommentOrderHot, model.CommentOrderLatest} {
		cursor := ""
		seen := map[string]bool{}
		var lastDate time.Time
		for n := 1; n <= 2; n++ {
			p, e := c.CommentsOrdered(ctx, saved.Credentials.Access, eid, cursor, order)
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("primary order=%s page=%d items=%d complete=%v cursor=%v", order, n, len(p.Items), p.Complete, p.Cursor != "")
			if order == model.CommentOrderHot {
				for _, item := range p.Items {
					if item.ReplyCount <= 0 || threadReads >= 2 {
						continue
					}
					threadReads++
					replies, e := c.CommentThread(ctx, saved.Credentials.Access, eid, item.ID, "")
					if e != nil {
						t.Fatal(e)
					}
					primaryRelations, replyReferences := 0, 0
					for _, reply := range replies.Items {
						if reply.PrimaryCommentID != "" {
							primaryRelations++
						}
						if reply.ReplyTo != nil {
							replyReferences++
						}
					}
					t.Logf("thread sample=%d expected=%d items=%d complete=%v primaryRelations=%d replyReferences=%d", threadReads, item.ReplyCount, len(replies.Items), replies.Complete, primaryRelations, replyReferences)
				}
			}
			if order == model.CommentOrderLatest {
				for _, item := range p.Items {
					date, _ := time.Parse(time.RFC3339Nano, item.CreatedAt)
					if !lastDate.IsZero() && date.After(lastDate) {
						t.Fatal("TIME ordering not descending in sampled pages")
					}
					lastDate = date
				}
			}
			if p.Cursor != "" {
				raw, e := decodeScopedCommentCursor(p.Cursor, eid, order)
				if e != nil {
					t.Fatal(e)
				}
				var fields map[string]json.RawMessage
				_ = json.Unmarshal(raw, &fields)
				keys := make([]string, 0, len(fields))
				for k := range fields {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				t.Logf("primary order=%s continuation fields=%v", order, keys)
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
	}
	if threadReads == 0 {
		t.Log("no existing replies in sampled HOT pages; live relationship decoder unverified")
	}
}
