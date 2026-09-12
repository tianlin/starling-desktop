package demoweb

import (
	"context"
	"encoding/json"
	"fmt"
	"starling/internal/model"
	"starling/internal/provider"
	"starling/internal/testkit"
)

func syntheticComment(i int) model.Comment {
	text := []string{"合成评论：这一段很适合慢慢听。\n第二行文字。", "<img src=x onerror=window.__commentXSS=1> 仅作为纯文本显示", "合成评论：谢谢分享这段日常。", "合成回复：我也有这样的感受。"}[i]
	c := model.Comment{ID: fmt.Sprintf("64db2d493fa4090b744c51%02d", i), Author: model.Identity{ID: "synthetic-listener", Nickname: "合成听众"}, Text: text, CreatedAt: "2026-09-12T08:00:00Z"}
	if i == 0 {
		c.ReplyCount = 1
	}
	return c
}

func installCommentFixtures(f *testkit.Fake) {
	page2 := provider.EncodeCursor(json.RawMessage(`{"page":2}`))
	f.CommentsFunc = func(_ context.Context, token, eid, cursor string) (model.CommentPage, error) {
		if cursor == "" {
			return model.CommentPage{Items: []model.Comment{syntheticComment(0), syntheticComment(1)}, Cursor: page2}, nil
		}
		if cursor != page2 {
			return model.CommentPage{}, model.Err("PAGINATION", "演示评论游标无效。")
		}
		return model.CommentPage{Items: []model.Comment{syntheticComment(1), syntheticComment(2)}, Complete: true}, nil
	}
	f.CommentThreadFunc = func(_ context.Context, token, eid, cid, cursor string) (model.CommentPage, error) {
		if cid != syntheticComment(0).ID || cursor != "" {
			return model.CommentPage{}, model.Err("NOT_FOUND", "演示回复不存在。")
		}
		return model.CommentPage{Items: []model.Comment{syntheticComment(3)}, Complete: true}, nil
	}
}
