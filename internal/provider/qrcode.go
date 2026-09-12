package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"starling/internal/model"
	"starling/internal/security"
)

type QRCode struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

func (c *Client) QRIdentity(ctx context.Context, token string) (model.Identity, error) {
	b, _, e := c.request(ctx, "GET", c.qrOrigin+"/web/user/get-me", nil, accessHeader(token), false)
	if e != nil {
		return model.Identity{}, e
	}
	return decodeIdentity(b)
}

// Public protocol observed in the official accounts site on 2026-09-12.
// The returned authorization link is displayed locally, never sent to a QR image service.
func (c *Client) CreateQR(ctx context.Context) (QRCode, error) {
	var q QRCode
	b, _, e := c.request(ctx, "POST", c.qrOrigin+"/v1/auth/qrcode/create", map[string]string{"clientId": "podcaster-platform"}, map[string]string{"x-midway-app-id": "v6worU4NnWyL"}, false)
	if e != nil {
		return q, e
	}
	if json.Unmarshal(b, &q) != nil || !security.ValidID(q.ID) {
		return QRCode{}, model.Err("BAD_RESPONSE", "平台未返回有效二维码。")
	}
	u, e := url.Parse(q.URL)
	if e != nil || u.Scheme != "https" || u.Host != "h5.xiaoyuzhoufm.com" || u.Path != "/oauth" || u.User != nil || u.Query().Get("qrcode_id") != q.ID {
		return QRCode{}, model.Err("BAD_RESPONSE", "平台二维码链接格式发生变化。")
	}
	return q, nil
}

func (c *Client) PollQR(ctx context.Context, id string) (string, model.Credentials, error) {
	empty := model.Credentials{}
	if !security.ValidID(id) {
		return "", empty, model.Err("INVALID_REQUEST", "二维码标识无效。")
	}
	b, h, e := c.request(ctx, "POST", c.qrOrigin+"/v1/auth/qrcode/login", map[string]string{"id": id}, nil, false)
	if e != nil {
		return "", empty, e
	}
	var result struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(b, &result) != nil {
		return "", empty, model.Err("BAD_RESPONSE", "扫码状态响应无效。")
	}
	switch result.Status {
	case "WAITTING", "SCANNED":
		return result.Status, empty, nil
	case "CONFIRMED", "USED":
		// The official UI treats both statuses as completed authorization.
		// Completion alone is insufficient: require credentials before connecting.
		creds := decodeCredentials(b, h, empty)
		response := http.Response{Header: h}
		for _, cookie := range response.Cookies() {
			if cookie.Name == "x-jike-access-token" {
				creds.Access = cookie.Value
			}
			if cookie.Name == "x-jike-refresh-token" {
				creds.Refresh = cookie.Value
			}
		}
		if !validToken(creds.Access) || !validToken(creds.Refresh) {
			return "", empty, model.Err("BAD_RESPONSE", "扫码已确认，但平台未返回完整会话。")
		}
		return "CONFIRMED", creds, nil
	default:
		return "", empty, model.Err("BAD_RESPONSE", "平台返回未知扫码状态。")
	}
}
