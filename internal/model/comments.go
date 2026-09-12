package model

import (
	"strings"
	"unicode/utf8"
)

type CommentOrder string

const (
	CommentOrderHot    CommentOrder = "hot"
	CommentOrderLatest CommentOrder = "latest"
)

func NormalizeCommentOrder(order CommentOrder) (CommentOrder, error) {
	switch order {
	case "", CommentOrderHot:
		return CommentOrderHot, nil
	case CommentOrderLatest:
		return CommentOrderLatest, nil
	default:
		return "", Err("INVALID_ORDER", "不支持该评论排序。")
	}
}

type Comment struct {
	ID               string            `json:"id"`
	Author           Identity          `json:"author"`
	Text             string            `json:"text"`
	CreatedAt        string            `json:"createdAt"`
	ReplyCount       int               `json:"replyCount,omitempty"`
	PrimaryCommentID string            `json:"primaryCommentId,omitempty"`
	ReplyTo          *CommentReference `json:"replyTo,omitempty"`
}

type CommentReference struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
	Summary  string `json:"summary"`
}

type CommentPage struct {
	Items    []Comment `json:"items"`
	Cursor   string    `json:"cursor"`
	Complete bool      `json:"complete"`
}

type CommentCreateResult struct {
	Comment Comment `json:"comment"`
}

// ValidateCommentText applies local input/transport safety, not a platform
// character limit. The upstream platform's text length limit is undocumented.
func ValidateCommentText(text string) error {
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" || strings.ContainsRune(text, 0) || len(text) > 1<<20 {
		return Err("INVALID_COMMENT", "请填写有效的评论正文；不能只含空白、包含空字符或超过本地 1 MiB 请求限制。")
	}
	return nil
}
