package provider

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"starling/internal/model"
	"starling/internal/security"
	"strings"
	"unicode"
)

type DiscoveryReader interface {
	Search(context.Context, string, string, model.SearchKind, string) (model.SearchPage, error)
	Suggestions(context.Context, string) ([]string, error)
	Creator(context.Context, string, string, string) (model.CreatorPage, error)
}
type discoveryCursor struct {
	Scope string          `json:"scope"`
	Key   json.RawMessage `json:"key"`
}

func discoveryBad() error {
	return model.Err("BAD_RESPONSE", "探索响应结构不符合已验证契约。")
}
func discoveryScope(q string, k model.SearchKind) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(string(k)+"\x00"+q)))
}
func validSearchKey(raw json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 {
		return false
	}
	var k struct {
		Offset *int   `json:"loadMoreKey"`
		ID     string `json:"searchId"`
	}
	if len(raw) > 2048 || json.Unmarshal(raw, &k) != nil || k.Offset == nil || *k.Offset < 0 || *k.Offset > 10000000 || k.ID == "" || len(k.ID) > 256 || strings.TrimSpace(k.ID) != k.ID {
		return false
	}
	for _, r := range k.ID {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func cleanDiscoveryText(s string, limit int) string {
	out := make([]rune, 0)
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return strings.TrimSpace(string(out))
}
func discoveryItem(raw json.RawMessage, kind string) (model.Item, error) {
	it, e := decodeItem(raw, kind)
	if e != nil {
		return it, e
	}
	it.MediaURL = ""
	it.Title = cleanDiscoveryText(it.Title, 1000)
	it.PodcastTitle = cleanDiscoveryText(it.PodcastTitle, 1000)
	it.Description = cleanDiscoveryText(it.Description, 20000)
	it.ShowNotes = ""
	it.Published = cleanDiscoveryText(it.Published, 100)
	if it.PodcastID != "" && !security.ValidID(it.PodcastID) {
		return model.Item{}, discoveryBad()
	}
	return it, nil
}
func decodeCreator(raw json.RawMessage) (model.Creator, error) {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || !security.ValidID(text(m, "uid")) {
		return model.Creator{}, discoveryBad()
	}
	return model.Creator{ID: text(m, "uid"), Nickname: cleanDiscoveryText(first(text(m, "nickname"), "小宇宙用户"), 1000), Bio: cleanDiscoveryText(text(m, "bio"), 10000), Avatar: security.ValidateImageURL(text(object(object(m, "avatar"), "picture"), "picUrl"))}, nil
}
func subscriptionFromRaw(raw json.RawMessage) model.SubscriptionState {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return model.SubscriptionUnknown
	}
	switch text(m, "subscriptionStatus") {
	case "ON":
		return model.SubscriptionOn
	case "OFF":
		return model.SubscriptionOff
	}
	return model.SubscriptionUnknown
}
func (c *Client) Search(ctx context.Context, token, q string, kind model.SearchKind, cursor string) (model.SearchPage, error) {
	out := model.SearchPage{Items: []model.Item{}, Users: []model.Creator{}, Subscriptions: map[string]model.SubscriptionState{}}
	if !validToken(token) {
		return out, model.Err("UNAUTHORIZED", "请先连接账号。")
	}
	q, e := model.NormalizeSearchQuery(q)
	if e != nil {
		return out, e
	}
	if e = model.ValidateSearchKind(kind); e != nil {
		return out, e
	}
	body := map[string]any{"limit": "20", "sourcePageName": "4", "currentPageName": "4", "type": strings.ToUpper(string(kind)), "keyword": q}
	scope := discoveryScope(q, kind)
	if cursor != "" {
		raw, e := DecodeCursor(cursor)
		var k discoveryCursor
		if e != nil || json.Unmarshal(raw, &k) != nil || k.Scope != scope || !validSearchKey(k.Key) {
			return out, model.Err("BAD_CURSOR", "搜索游标与当前查询不匹配。")
		}
		body["loadMoreKey"] = k.Key
	}
	b, _, e := c.request(ctx, "POST", c.api+"/v1/search/create", body, accessHeader(token), true)
	if e != nil {
		return out, e
	}
	var env map[string]json.RawMessage
	if json.Unmarshal(b, &env) != nil {
		return out, discoveryBad()
	}
	items, next, complete, e := DecodePage(env)
	if e != nil {
		return out, e
	}
	if len(items) > 200 {
		return out, discoveryBad()
	}
	// Live-checked for PODCAST, EPISODE and USER on 2026-09-12:
	// search omits both paging fields on terminal pages, including empty
	// results. Keep this endpoint contract out of the generic decoder.
	_, hasKey := env["loadMoreKey"]
	_, hasMore := env["hasMore"]
	if !hasKey && !hasMore {
		complete = true
	}
	if next != "" {
		key, e := DecodeCursor(next)
		if e != nil || !validSearchKey(key) {
			return out, discoveryBad()
		}
		raw, _ := json.Marshal(discoveryCursor{scope, key})
		out.Cursor = EncodeCursor(raw)
		if cursor == out.Cursor {
			return out, model.Err("PAGINATION", "搜索游标未前进。")
		}
	}
	out.Complete = complete
	seen := map[string]bool{}
	rawUsers := 0
	for _, raw := range items {
		if kind == model.SearchUser {
			var m map[string]json.RawMessage
			if json.Unmarshal(raw, &m) != nil {
				return out, discoveryBad()
			}
			users := []json.RawMessage{raw}
			var typ string
			json.Unmarshal(m["type"], &typ)
			if typ == "SEARCHED_USERS" {
				if json.Unmarshal(m["users"], &users) != nil || users == nil || len(users) > 200 {
					return out, discoveryBad()
				}
			}
			rawUsers += len(users)
			if rawUsers > 200 {
				return out, discoveryBad()
			}
			for _, u := range users {
				cr, e := decodeCreator(u)
				if e != nil {
					return out, e
				}
				if !seen[cr.ID] {
					out.Users = append(out.Users, cr)
					seen[cr.ID] = true
				}
				if len(out.Users) > 200 {
					return out, discoveryBad()
				}
			}
			continue
		}
		it, e := discoveryItem(raw, string(kind))
		if e != nil {
			return out, e
		}
		if seen[it.Key()] {
			continue
		}
		seen[it.Key()] = true
		out.Items = append(out.Items, it)
		if it.PodcastID != "" {
			sr := raw
			if kind == model.SearchEpisode {
				var m map[string]json.RawMessage
				json.Unmarshal(raw, &m)
				sr = m["podcast"]
			}
			out.Subscriptions[it.PodcastID] = subscriptionFromRaw(sr)
		}
	}
	if out.Cursor != "" && len(out.Items)+len(out.Users) == 0 {
		return out, model.Err("PAGINATION", "搜索返回空页但仍要求继续，已停止加载。")
	}
	return out, nil
}
func (c *Client) Suggestions(ctx context.Context, token string) ([]string, error) {
	out := []string{}
	if !validToken(token) {
		return out, model.Err("UNAUTHORIZED", "请先连接账号。")
	}
	b, _, e := c.request(ctx, "GET", c.api+"/v1/search/get-preset", nil, accessHeader(token), true)
	if e != nil {
		return out, e
	}
	var env struct {
		Data []struct {
			Text string `json:"text"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &env) != nil || env.Data == nil || len(env.Data) > 100 {
		return out, discoveryBad()
	}
	seen := map[string]bool{}
	for _, r := range env.Data {
		q, e := model.NormalizeSearchQuery(r.Text)
		if e != nil {
			return nil, discoveryBad()
		}
		if !seen[q] {
			seen[q] = true
			out = append(out, q)
		}
	}
	return out, nil
}
func (c *Client) Creator(ctx context.Context, token, uid, cursor string) (model.CreatorPage, error) {
	out := model.CreatorPage{Items: []model.Item{}, Subscriptions: map[string]model.SubscriptionState{}}
	if !validToken(token) {
		return out, model.Err("UNAUTHORIZED", "请先连接账号。")
	}
	if !security.ValidID(uid) {
		return out, model.Err("INVALID_ID", "用户 ID 无效。")
	}
	if cursor != "" {
		return out, model.Err("BAD_CURSOR", "作者节目列表不支持继续分页。")
	}
	b, _, e := c.request(ctx, "GET", c.api+"/v1/profile/get?uid="+uid, nil, accessHeader(token), true)
	if e != nil {
		return out, e
	}
	var env map[string]json.RawMessage
	if json.Unmarshal(b, &env) != nil {
		return out, discoveryBad()
	}
	out.Creator, e = decodeCreator(env["data"])
	if e != nil {
		return out, e
	}
	if out.Creator.ID != uid {
		return out, discoveryBad()
	}
	b, _, e = c.request(ctx, "POST", c.api+"/v1/podcaster/owned-podcasts", map[string]string{"uid": uid}, accessHeader(token), true)
	if e != nil {
		return out, e
	}
	env = nil
	if json.Unmarshal(b, &env) != nil {
		return out, discoveryBad()
	}
	items, next, _, e := DecodePage(env)
	if e != nil {
		return out, e
	}
	if next != "" || len(items) > 200 {
		return out, discoveryBad()
	}
	seen := map[string]bool{}
	for _, raw := range items {
		it, e := discoveryItem(raw, "podcast")
		if e != nil {
			return out, e
		}
		if !seen[it.ID] {
			seen[it.ID] = true
			out.Items = append(out.Items, it)
			out.Subscriptions[it.ID] = subscriptionFromRaw(raw)
		}
	}
	out.Complete = true
	return out, nil
}
