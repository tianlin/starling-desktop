package provider

import (
	"encoding/json"
	"net/http"
	"starling/internal/model"
	"starling/internal/security"
	"strings"
)

func decodeCredentials(b []byte, h http.Header, fallback model.Credentials) model.Credentials {
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if data, ok := m["data"].(map[string]any); ok {
		for k, v := range data {
			if _, exists := m[k]; !exists {
				m[k] = v
			}
		}
	}
	access := h.Get("x-jike-access-token")
	refresh := h.Get("x-jike-refresh-token")
	if access == "" {
		access = text(m, "x-jike-access-token")
	}
	if refresh == "" {
		refresh = text(m, "x-jike-refresh-token")
	}
	if access == "" {
		access = text(m, "accessToken")
	}
	if refresh == "" {
		refresh = text(m, "refreshToken")
	}
	if refresh == "" {
		refresh = fallback.Refresh
	}
	return model.Credentials{Access: access, Refresh: refresh}
}
func decodeIdentity(b []byte) (model.Identity, error) {
	var root map[string]any
	if json.Unmarshal(b, &root) != nil {
		return model.Identity{}, model.Err("BAD_RESPONSE", "账号身份响应无效。")
	}
	data := object(root, "data")
	if user := object(data, "user"); user != nil {
		data = user
	}
	id := text(data, "uid")
	nick := text(data, "nickname")
	if id == "" || len(id) > 128 || strings.ContainsAny(id, "\x00\r\n") {
		return model.Identity{}, model.Err("BAD_RESPONSE", "平台未提供可核对的账号标识。")
	}
	if nick == "" {
		nick = "小宇宙用户"
	}
	return model.Identity{ID: id, Nickname: nick, Avatar: security.ValidateImageURL(text(object(object(data, "avatar"), "picture"), "picUrl"))}, nil
}
func decodeItem(raw json.RawMessage, kind string) (model.Item, error) {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return model.Item{}, model.Err("BAD_RESPONSE", "内容详情结构无效。")
	}
	idKey := "eid"
	if kind == "podcast" {
		idKey = "pid"
	}
	if text(m, idKey) == "" {
		if sub := object(m, kind); sub != nil {
			m = sub
		}
	}
	id := text(m, idKey)
	if !security.ValidID(id) {
		return model.Item{}, model.Err("BAD_RESPONSE", "内容缺少有效的稳定 ID，未把该页视作完整。")
	}
	title := text(m, "title")
	if title == "" {
		title = "内容暂不可用"
	}
	it := model.Item{Kind: kind, ID: id, Title: title, Description: text(m, "description"), ShowNotes: text(m, "shownotes"), SourceURL: "https://www.xiaoyuzhoufm.com/" + kind + "/" + id, Published: text(m, "pubDate")}
	if f, ok := m["duration"].(float64); ok && f > 0 && f < 7*86400 {
		it.Duration = f
	}
	pic := object(m, "image")
	it.Image = security.ValidateImageURL(first(text(pic, "middlePicUrl"), text(pic, "picUrl"), text(pic, "largePicUrl")))
	if podcast := object(m, "podcast"); podcast != nil {
		it.PodcastID = text(podcast, "pid")
		it.PodcastTitle = text(podcast, "title")
	}
	if kind == "podcast" {
		it.PodcastID = id
		it.PodcastTitle = title
	}
	pay := strings.ToUpper(text(m, "payType"))
	it.Restricted = (pay != "" && pay != "FREE") || boolean(m, "isPrivate") || boolean(m, "isDeleted")
	if it.Restricted {
		it.Restriction = "首版不播放付费、私密或下架内容，请在官方客户端确认权限。"
	}
	media := object(m, "media")
	it.MediaURL = first(text(object(m, "enclosure"), "url"), text(object(media, "source"), "url"), text(media, "url"))
	if kind == "episode" && it.MediaURL == "" && boolean(m, "isPlayable") == false {
		if v, exists := m["isPlayable"]; exists && v == false {
			it.Restricted = true
			it.Restriction = "平台标记该内容暂不可播放。"
		}
	}
	return it, nil
}
func text(m map[string]any, k string) string           { s, _ := m[k].(string); return s }
func object(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }
func boolean(m map[string]any, k string) bool          { v, _ := m[k].(bool); return v }
func first(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
