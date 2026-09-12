package model

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type SearchKind string

const (
	SearchPodcast SearchKind = "podcast"
	SearchEpisode SearchKind = "episode"
	SearchUser    SearchKind = "user"
)

func NormalizeSearchQuery(s string) (string, error) {
	if !utf8.ValidString(s) {
		return "", Err("INVALID_REQUEST", "搜索内容无效。")
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", Err("INVALID_REQUEST", "搜索内容不能包含控制字符。")
		}
	}
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > 200 {
		return "", Err("INVALID_REQUEST", "搜索内容须为 1–200 个字符。")
	}
	return s, nil
}
func ValidateSearchKind(k SearchKind) error {
	switch k {
	case SearchPodcast, SearchEpisode, SearchUser:
		return nil
	}
	return Err("INVALID_REQUEST", "不支持该搜索类别。")
}

type SubscriptionState string

const (
	SubscriptionOn      SubscriptionState = "subscribed"
	SubscriptionOff     SubscriptionState = "not_subscribed"
	SubscriptionUnknown SubscriptionState = "unknown"
)

type Creator struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar,omitempty"`
	Bio      string `json:"bio,omitempty"`
}
type SearchPage struct {
	Items         []Item                       `json:"items"`
	Users         []Creator                    `json:"users"`
	Subscriptions map[string]SubscriptionState `json:"subscriptions"`
	Cursor        string                       `json:"cursor"`
	Complete      bool                         `json:"complete"`
}
type CreatorPage struct {
	Creator       Creator                      `json:"creator"`
	Items         []Item                       `json:"items"`
	Subscriptions map[string]SubscriptionState `json:"subscriptions"`
	Cursor        string                       `json:"cursor"`
	Complete      bool                         `json:"complete"`
}
type SubscriptionResult struct {
	PodcastID string            `json:"podcastId"`
	State     SubscriptionState `json:"state"`
	Item      *Item             `json:"item,omitempty"`
	Warning   *AppError         `json:"warning,omitempty"`
}
