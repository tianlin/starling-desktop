package provider

import (
	"encoding/json"
	"starling/internal/model"
	"testing"
)

func TestCommentThreadRemovedReference(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		bad    bool
	}{
		{"removed without text", func(m map[string]any) {}, false},
		{"removed null text", func(m map[string]any) { m["text"] = nil }, false},
		{"removed stale text is hidden", func(m map[string]any) { m["text"] = "stale private text" }, false},
		{"normal without text", func(m map[string]any) { m["status"] = "NORMAL" }, true},
		{"unknown without text", func(m map[string]any) { m["status"] = "UNKNOWN" }, true},
		{"missing status and text", func(m map[string]any) { delete(m, "status") }, true},
		{"malformed text", func(m map[string]any) { m["text"] = 42 }, true},
		{"foreign episode", func(m map[string]any) { m["owner"].(map[string]any)["id"] = "66467d2c251bd96e6cdcdddf" }, true},
		{"foreign thread", func(m map[string]any) { m["thread"] = "66469a168029f4442e95a227" }, true},
		{"invalid id", func(m map[string]any) { m["id"] = "bad" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var row map[string]any
			if e := json.Unmarshal([]byte(replyFixture(commentID)), &row); e != nil {
				t.Fatal(e)
			}
			ref := row["replyToComment"].(map[string]any)
			delete(ref, "text")
			ref["status"] = "REMOVED"
			ref["thread"] = commentID
			tc.change(ref)
			raw, _ := json.Marshal(map[string]any{"data": []any{row}, "totalCount": 22, "loadMoreKey": map[string]any{"id": replyID}})
			page, e := decodeCommentPage(raw, commentEpisode, true)
			if tc.bad {
				if !model.IsCode(e, "BAD_RESPONSE") {
					t.Fatalf("expected rejection: %v", e)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if len(page.Items) != 1 || page.Complete || page.Cursor != "" {
				t.Fatalf("partial page lost: %+v", page)
			}
			got := page.Items[0]
			if got.Text != "<b>plain text</b>" || got.PrimaryCommentID != commentID || got.ReplyTo == nil || got.ReplyTo.ID != commentID || got.ReplyTo.Nickname != "Target" || got.ReplyTo.Summary != "原评论已删除" {
				t.Fatalf("reply/reference mismatch: %+v", got)
			}
		})
	}
}
