package model

type Comment struct {
	ID         string   `json:"id"`
	Author     Identity `json:"author"`
	Text       string   `json:"text"`
	CreatedAt  string   `json:"createdAt"`
	ReplyCount int      `json:"replyCount,omitempty"`
}

type CommentPage struct {
	Items    []Comment `json:"items"`
	Cursor   string    `json:"cursor"`
	Complete bool      `json:"complete"`
}
