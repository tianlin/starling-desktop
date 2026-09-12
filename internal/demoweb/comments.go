package demoweb

import (
	"context"
	"encoding/json"
	"fmt"
	"starling/internal/model"
	"starling/internal/provider"
	"starling/internal/testkit"
	"sync"
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
	var mu sync.Mutex
	posted := map[string][]model.Comment{}
	sequence := 0
	page2 := provider.EncodeCursor(json.RawMessage(`{"page":2}`))
	f.CommentsFunc = func(_ context.Context, token, eid, cursor string) (model.CommentPage, error) {
		mu.Lock()
		defer mu.Unlock()
		if cursor == "" {
			items := append([]model.Comment{}, posted[eid]...)
			items = append(items, syntheticComment(0), syntheticComment(1))
			return model.CommentPage{Items: items, Cursor: page2}, nil
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
	f.CreateCommentFunc = func(ctx context.Context, token, eid, text string) (model.Comment, error) {
		if err := model.ValidateCommentText(text); err != nil {
			return model.Comment{}, err
		}
		if ctx.Err() != nil {
			return model.Comment{}, model.Err("COMMENT_UNCERTAIN", "合成发表结果未确认。")
		}
		mu.Lock()
		defer mu.Unlock()
		sequence++
		comment := model.Comment{ID: fmt.Sprintf("%024x", sequence), Author: model.Identity{ID: "user-a", Nickname: "合成测试账号"}, Text: text, CreatedAt: model.Now()}
		posted[eid] = append([]model.Comment{comment}, posted[eid]...)
		return comment, nil
	}
}
