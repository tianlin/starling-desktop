package provider

import (
	"context"
	"encoding/json"
	"starling/internal/model"
	"starling/internal/security"
	"time"
)

// CommentReader is optional; providers without account comments remain compatible.
type CommentReader interface {
	Comments(context.Context, string, string, string) (model.CommentPage, error)
	CommentThread(context.Context, string, string, string, string) (model.CommentPage, error)
}

type SortedCommentReader interface {
	CommentsOrdered(context.Context, string, string, string, model.CommentOrder) (model.CommentPage, error)
}

func (c *Client) Comments(ctx context.Context, token, episodeID, cursor string) (model.CommentPage, error) {
	return c.CommentsOrdered(ctx, token, episodeID, cursor, model.CommentOrderHot)
}
func (c *Client) CommentsOrdered(ctx context.Context, token, episodeID, cursor string, order model.CommentOrder) (model.CommentPage, error) {
	order, e := model.NormalizeCommentOrder(order)
	if e != nil {
		return model.CommentPage{}, e
	}
	return c.readComments(ctx, token, episodeID, "", cursor, order)
}
func (c *Client) CommentThread(ctx context.Context, token, episodeID, commentID, cursor string) (model.CommentPage, error) {
	if !security.ValidID(commentID) {
		return model.CommentPage{}, model.Err("INVALID_ID", "评论 ID 无效。")
	}
	return c.readComments(ctx, token, episodeID, commentID, cursor, model.CommentOrderHot)
}
func (c *Client) readComments(ctx context.Context, token, episodeID, commentID, cursor string, order model.CommentOrder) (model.CommentPage, error) {
	if !validToken(token) {
		return model.CommentPage{}, model.Err("UNAUTHORIZED", "请连接账号后读取评论。")
	}
	if !security.ValidID(episodeID) {
		return model.CommentPage{}, model.Err("INVALID_ID", "单集 ID 无效。")
	}
	thread := commentID != ""
	body := map[string]any{"owner": map[string]string{"id": episodeID, "type": "EPISODE"}, "order": "HOT"}
	if order == model.CommentOrderLatest {
		body["order"] = "TIME"
	}
	path := "/v1/comment/list-primary"
	if thread {
		if cursor != "" {
			return model.CommentPage{}, model.Err("UNSUPPORTED", "尚未验证回复分页接口，请在官方客户端查看其余回复。")
		}
		path = "/v1/comment/list-thread"
		body = map[string]any{"primaryCommentId": commentID, "order": "SMART"}
	} else if cursor != "" {
		key, e := decodeScopedCommentCursor(cursor, episodeID, order)
		if e != nil {
			return model.CommentPage{}, e
		}
		body["loadMoreKey"] = key
	}
	b, _, e := c.request(ctx, "POST", c.api+path, body, accessHeader(token), true)
	if e != nil {
		return model.CommentPage{}, e
	}
	return decodeCommentPageOrdered(b, episodeID, thread, order)
}

func validCommentCursor(raw json.RawMessage, order model.CommentOrder) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return false
	}
	var id, direction, section string
	var score *float64
	if len(raw) > 5000 || json.Unmarshal(m["id"], &id) != nil || !security.ValidID(id) || json.Unmarshal(m["direction"], &direction) != nil || direction != "NEXT" {
		return false
	}
	if order == model.CommentOrderHot {
		if json.Unmarshal(m["hotSortScore"], &score) != nil || score == nil {
			return false
		}
	} else if v, ok := m["hotSortScore"]; ok {
		if json.Unmarshal(v, &score) != nil || score == nil {
			return false
		}
	}
	if v, ok := m["section"]; ok && json.Unmarshal(v, &section) != nil {
		return false
	}
	return true
}

func decodeCommentPage(b []byte, episodeID string, thread bool) (model.CommentPage, error) {
	return decodeCommentPageOrdered(b, episodeID, thread, model.CommentOrderHot)
}

type scopedCommentCursor struct {
	EpisodeID string             `json:"episodeID"`
	Order     model.CommentOrder `json:"order"`
	Raw       json.RawMessage    `json:"raw"`
}

func decodeScopedCommentCursor(cursor, episodeID string, order model.CommentOrder) (json.RawMessage, error) {
	raw, e := DecodeCursor(cursor)
	if e != nil {
		return nil, e
	}
	var scope scopedCommentCursor
	if json.Unmarshal(raw, &scope) != nil || scope.EpisodeID != episodeID || scope.Order != order || !validCommentCursor(scope.Raw, order) {
		return nil, model.Err("BAD_CURSOR", "评论分页游标不属于当前单集或排序，或结构不受支持。")
	}
	return scope.Raw, nil
}

func decodeCommentPageOrdered(b []byte, episodeID string, thread bool, order model.CommentOrder) (model.CommentPage, error) {
	var env map[string]json.RawMessage
	bad := func() (model.CommentPage, error) {
		return model.CommentPage{}, model.Err("BAD_RESPONSE", "评论响应结构不符合已验证契约。")
	}
	if json.Unmarshal(b, &env) != nil || env == nil {
		return bad()
	}
	items, cursor, complete, e := DecodePage(env)
	if e != nil {
		return model.CommentPage{}, e
	}
	if cursor != "" {
		// Thread pagination has no verified request contract. Preserve partial state,
		// without exposing a cursor the client cannot correctly consume.
		if thread {
			cursor = ""
			complete = false
		} else {
			key, e := DecodeCursor(cursor)
			if e != nil || !validCommentCursor(key, order) {
				return bad()
			}
			raw, e := json.Marshal(scopedCommentCursor{EpisodeID: episodeID, Order: order, Raw: key})
			if e != nil {
				return bad()
			}
			cursor = EncodeCursor(raw)
		}
	}
	if !thread {
		if _, ok := env["loadMoreKey"]; !ok {
			if next, exists := env["loadNextKey"]; exists && string(next) != "null" && string(next) != `""` {
				return model.CommentPage{}, model.Err("PAGINATION", "平台只返回了未经验证的下一页字段，已停止加载。")
			}
			if _, hasMore := env["hasMore"]; !hasMore {
				complete = true
			}
		}
	}
	var total *int
	if raw, ok := env["totalCount"]; ok {
		if json.Unmarshal(raw, &total) != nil || total == nil || *total < 0 {
			return bad()
		}
		if thread && cursor == "" {
			if *total < len(items) {
				return bad()
			}
			if _, hasKey := env["loadMoreKey"]; !hasKey {
				if _, hasMore := env["hasMore"]; !hasMore {
					complete = *total == len(items)
				}
			}
		}
	}
	out := model.CommentPage{Items: make([]model.Comment, 0, len(items)), Cursor: cursor, Complete: complete}
	seen := map[string]bool{}
	for _, raw := range items {
		var v struct {
			ID    string `json:"id"`
			Owner struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"owner"`
			Author     json.RawMessage `json:"author"`
			Text       *string         `json:"text"`
			CreatedAt  string          `json:"createdAt"`
			ReplyCount int             `json:"threadReplyCount"`
		}
		if json.Unmarshal(raw, &v) != nil || !security.ValidID(v.ID) || seen[v.ID] || v.Owner.ID != episodeID || v.Owner.Type != "EPISODE" || v.Text == nil || v.ReplyCount < 0 {
			return bad()
		}
		if _, e := time.Parse(time.RFC3339Nano, v.CreatedAt); e != nil {
			return bad()
		}
		author, e := decodeIdentity(append(append([]byte(`{"data":`), v.Author...), '}'))
		if e != nil {
			return bad()
		}
		seen[v.ID] = true
		out.Items = append(out.Items, model.Comment{ID: v.ID, Author: author, Text: *v.Text, CreatedAt: v.CreatedAt, ReplyCount: v.ReplyCount})
	}
	return out, nil
}
