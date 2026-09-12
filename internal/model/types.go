// Package model defines the normalized, provider-independent business contract.
package model

import (
	"errors"
	"fmt"
	"time"
)

type AppError struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	RetryAfter   int    `json:"retryAfter,omitempty"`
	HTTPStatus   int    `json:"httpStatus,omitempty"`
	UpstreamCode int    `json:"upstreamCode,omitempty"`
}

func (e *AppError) Error() string      { return e.Code + ": " + e.Message }
func Err(code, msg string) error       { return &AppError{Code: code, Message: msg} }
func IsCode(e error, code string) bool { var x *AppError; return errors.As(e, &x) && x.Code == code }
func PublicError(e error) *AppError {
	var x *AppError
	if errors.As(e, &x) {
		return x
	}
	return &AppError{Code: "INTERNAL", Message: "操作未完成，请重试或查看本地诊断。"}
}

var ErrStale = Err("STALE_SESSION", "账号或页面已改变，已丢弃旧操作。")

type Identity struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar,omitempty"`
}
type Credentials struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
}
type SavedSession struct {
	Credentials Credentials `json:"credentials"`
	Identity    Identity    `json:"identity"`
}
type SessionView struct {
	Epoch            uint64    `json:"epoch"`
	State            string    `json:"state"`
	Identity         *Identity `json:"identity,omitempty"`
	Persistent       bool      `json:"persistent"`
	StorageAvailable bool      `json:"storageAvailable"`
}

type Item struct {
	Kind         string  `json:"kind"`
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	PodcastID    string  `json:"podcastId,omitempty"`
	PodcastTitle string  `json:"podcastTitle,omitempty"`
	Description  string  `json:"description,omitempty"`
	ShowNotes    string  `json:"showNotes,omitempty"`
	Image        string  `json:"image,omitempty"`
	Duration     float64 `json:"duration,omitempty"`
	Published    string  `json:"published,omitempty"`
	SourceURL    string  `json:"sourceUrl"`
	Restricted   bool    `json:"restricted"`
	Restriction  string  `json:"restriction,omitempty"`
	// Signed media URLs never enter metadata persistence or generic UI state.
	MediaURL string `json:"-"`
}

func (i Item) Key() string { return i.Kind + ":" + i.ID }

type Page struct {
	Items    []Item `json:"items"`
	Cursor   string `json:"cursor"`
	Complete bool   `json:"complete"`
}
type LibraryView struct {
	Items     []Item    `json:"items"`
	Cursor    string    `json:"cursor"`
	Status    string    `json:"status"`
	Complete  bool      `json:"complete"`
	UpdatedAt string    `json:"updatedAt,omitempty"`
	Error     *AppError `json:"error,omitempty"`
	Pages     int       `json:"pages"`
	Revision  uint64    `json:"revision"`
	Epoch     uint64    `json:"epoch"`
}
type Playback struct {
	Item     Item    `json:"item"`
	URL      string  `json:"url"`
	Epoch    uint64  `json:"epoch"`
	Position float64 `json:"position"`
}
type Progress struct {
	Item      Item    `json:"item"`
	Position  float64 `json:"position"`
	Duration  float64 `json:"duration"`
	Ended     bool    `json:"ended"`
	UpdatedAt string  `json:"updatedAt"`
}
type Settings struct {
	Volume              float64 `json:"volume"`
	Rate                float64 `json:"rate"`
	CloseBehavior       string  `json:"closeBehavior"`
	ExperimentalAccount bool    `json:"experimentalAccount"`
}

func DefaultSettings() Settings { return Settings{Volume: .8, Rate: 1, CloseBehavior: "ask"} }
func (s Settings) Validate() error {
	if s.Volume < 0 || s.Volume > 1 || s.Volume != s.Volume {
		return Err("INVALID_SETTINGS", "音量必须在 0 到 1 之间。")
	}
	valid := false
	for _, v := range []float64{.75, 1, 1.25, 1.5, 2} {
		if s.Rate == v {
			valid = true
		}
	}
	if !valid {
		return Err("INVALID_SETTINGS", "不支持的播放速度。")
	}
	switch s.CloseBehavior {
	case "ask", "tray", "exit":
	default:
		return Err("INVALID_SETTINGS", "不支持的关闭行为。")
	}
	return nil
}
func Now() string             { return time.Now().UTC().Format(time.RFC3339Nano) }
func Scope(i Identity) string { return fmt.Sprintf("xiaoyuzhou:%s", i.ID) }
