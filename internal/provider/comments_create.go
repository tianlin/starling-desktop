package provider

import (
	"context"
	"encoding/json"
	"starling/internal/model"
	"starling/internal/security"
)

// CommentWriter is deliberately optional and only supports plain-text,
// top-level comments; callers must not automatically retry this mutation.
type CommentWriter interface {
	CreateComment(context.Context, string, string, string) (model.Comment, error)
}

func (c *Client) CreateComment(ctx context.Context, token, episodeID, text string) (model.Comment, error) {
	if !validToken(token) {
		return model.Comment{}, model.Err("UNAUTHORIZED", "请连接账号后发表评论。")
	}
	if !security.ValidID(episodeID) {
		return model.Comment{}, model.Err("INVALID_ID", "单集 ID 无效。")
	}
	if e := model.ValidateCommentText(text); e != nil {
		return model.Comment{}, e
	}
	body := map[string]any{"text": text, "owner": map[string]string{"id": episodeID, "type": "EPISODE"}}
	b, _, e := c.request(ctx, "POST", c.api+"/v1/comment/create", body, accessHeader(token), false)
	if e != nil {
		switch model.PublicError(e).Code {
		case "NETWORK", "CANCELLED", "UPSTREAM", "BAD_RESPONSE", "REDIRECT_BLOCKED", "INTERNAL":
			return model.Comment{}, commentUncertain()
		default:
			return model.Comment{}, e
		}
	}
	comment, e := decodeCreatedComment(b, episodeID)
	if e != nil {
		return model.Comment{}, commentUncertain()
	}
	return comment, nil
}

func commentUncertain() error {
	return model.Err("COMMENT_UNCERTAIN", "发表结果未确认，请刷新评论或在官方客户端核对；不会自动重发。")
}

func decodeCreatedComment(b []byte, episodeID string) (model.Comment, error) {
	var env map[string]json.RawMessage
	bad := func() (model.Comment, error) {
		return model.Comment{}, model.Err("BAD_RESPONSE", "发表响应结构不符合已验证契约。")
	}
	if json.Unmarshal(b, &env) != nil {
		return bad()
	}
	var item map[string]json.RawMessage
	if json.Unmarshal(env["data"], &item) != nil || item == nil {
		return bad()
	}
	// The create endpoint documents replyCount, while list-primary documents
	// threadReplyCount. Normalize into the shared decoder without guessing aliases.
	if raw, ok := item["replyCount"]; ok {
		var count *int
		if json.Unmarshal(raw, &count) != nil || count == nil || *count < 0 {
			return bad()
		}
		item["threadReplyCount"] = raw
	}
	raw, e := json.Marshal(item)
	if e != nil {
		return bad()
	}
	page, e := decodeCommentPage(append(append([]byte(`{"data":[`), raw...), []byte(`],"totalCount":1}`)...), episodeID, true)
	if e != nil || len(page.Items) != 1 {
		return bad()
	}
	return page.Items[0], nil
}
