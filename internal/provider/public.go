package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"starling/internal/model"
)

var nextData = regexp.MustCompile(`(?s)<script\b[^>]*\bid=["']__NEXT_DATA__["'][^>]*>(.*?)</script\s*>`)

func (c *Client) publicData(ctx context.Context, kind, id string) (map[string]json.RawMessage, error) {
	b, _, e := c.request(ctx, "GET", c.web+"/"+kind+"/"+id, nil, nil, true)
	if e != nil {
		return nil, e
	}
	matches := nextData.FindSubmatch(b)
	if len(matches) != 2 {
		return nil, model.Err("PUBLIC_UNAVAILABLE", "公开页面结构不受支持，请使用官方页面收听。")
	}
	var v struct {
		Props struct {
			PageProps map[string]json.RawMessage `json:"pageProps"`
		} `json:"props"`
	}
	if json.Unmarshal(matches[1], &v) != nil || v.Props.PageProps == nil {
		return nil, model.Err("BAD_RESPONSE", "公开页面内容无法解析。")
	}
	return v.Props.PageProps, nil
}
func (c *Client) publicDetail(ctx context.Context, kind, id string) (model.Item, error) {
	data, e := c.publicData(ctx, kind, id)
	if e != nil {
		return model.Item{}, e
	}
	return decodeItem(data[kind], kind)
}
func (c *Client) publicEpisodes(ctx context.Context, pid string) (model.Page, error) {
	data, e := c.publicData(ctx, "podcast", pid)
	if e != nil {
		return model.Page{}, e
	}
	raw, ok := data["episodes"]
	if !ok {
		// Current public pages nest the preview under podcast. Keep support for
		// the older top-level shape, but never mask a malformed present value.
		if podcast, exists := data["podcast"]; exists {
			var nested map[string]json.RawMessage
			if json.Unmarshal(podcast, &nested) != nil || nested == nil {
				return model.Page{}, model.Err("BAD_RESPONSE", "公开节目结构变化。")
			}
			raw, ok = nested["episodes"]
		}
	}
	if !ok {
		return model.Page{}, model.Err("PUBLIC_UNAVAILABLE", "公开页面没有提供单集列表，连接账号后再尝试。")
	}
	var list []json.RawMessage
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' || json.Unmarshal(trimmed, &list) != nil {
		return model.Page{}, model.Err("BAD_RESPONSE", "公开单集列表结构变化。")
	}
	p := model.Page{Items: make([]model.Item, 0, len(list)), Complete: false}
	for _, v := range list {
		it, e := decodeItem(v, "episode")
		if e != nil {
			return model.Page{}, e
		}
		p.Items = append(p.Items, it)
	}
	return p, nil
}
