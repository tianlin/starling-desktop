package provider

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"starling/internal/model"
	"starling/internal/security"
	"strconv"
	"strings"
	"sync"
	"time"
)

const AdapterVersion = "xyz-updates-2026-09-12.1"
const userAgent = "Starling/0.1.0-alpha (unofficial desktop podcast client)"

type Client struct {
	http           *http.Client
	api, auth, web string
	qrOrigin       string
	deviceID       string
	deviceErr      error
	mu             sync.Mutex
	cooldown       map[string]time.Time
	slots          chan struct{}
}

func New() *Client {
	transport := &http.Transport{TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, IdleConnTimeout: 60 * time.Second, MaxIdleConns: 8, MaxConnsPerHost: 3}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(addr)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, e
		}
		for _, ip := range ips {
			if !security.PublicIP(ip.IP) {
				return nil, errors.New("non-public destination blocked")
			}
		}
		d := net.Dialer{Timeout: 10 * time.Second}
		for _, ip := range ips {
			c, e := d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if e == nil {
				return c, nil
			}
		}
		return nil, errors.New("connection failed")
	}
	return newClient(&http.Client{Transport: transport, Timeout: 25 * time.Second}, "https://api.xiaoyuzhoufm.com", "https://podcaster-api.xiaoyuzhoufm.com", "https://www.xiaoyuzhoufm.com")
}
func newClient(h *http.Client, api, auth, web string) *Client {
	cp := *h
	cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	// A random UUID identifies this client lifetime only; no hardware or mobile fingerprint.
	var id [16]byte
	_, deviceErr := rand.Read(id[:])
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	deviceID := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	return &Client{http: &cp, api: api, auth: auth, web: web, qrOrigin: "https://web-api.xiaoyuzhoufm.com", deviceID: deviceID, deviceErr: deviceErr, cooldown: map[string]time.Time{}, slots: make(chan struct{}, 3)}
}
func validToken(s string) bool {
	return s != "" && len(s) <= 16384 && !strings.ContainsAny(s, "\r\n\x00")
}

var phonePattern = regexp.MustCompile(`^[0-9]{5,15}$`)
var areaPattern = regexp.MustCompile(`^\+[0-9]{1,4}$`)
var codePattern = regexp.MustCompile(`^[0-9]{4,8}$`)

func ValidatePhone(phone, area string) error {
	if !phonePattern.MatchString(phone) || !areaPattern.MatchString(area) {
		return model.Err("INVALID_PHONE", "请填写有效手机号和国家区号。")
	}
	return nil
}
func (c *Client) SendCode(ctx context.Context, phone, area string) error {
	if e := ValidatePhone(phone, area); e != nil {
		return e
	}
	_, _, e := c.request(ctx, "POST", c.auth+"/v1/auth/send-code", map[string]any{"mobilePhoneNumber": phone, "areaCode": area}, nil, false)
	return e
}
func (c *Client) Login(ctx context.Context, phone, area, code string) (model.Credentials, model.Identity, error) {
	var creds model.Credentials
	var id model.Identity
	if e := ValidatePhone(phone, area); e != nil {
		return creds, id, e
	}
	if !codePattern.MatchString(code) {
		return creds, id, model.Err("INVALID_CODE", "验证码应为 4–8 位数字。")
	}
	b, h, e := c.request(ctx, "POST", c.auth+"/v1/auth/login-with-sms", map[string]any{"mobilePhoneNumber": phone, "areaCode": area, "verifyCode": code}, nil, false)
	if e != nil {
		return creds, id, e
	}
	creds = decodeCredentials(b, h, model.Credentials{})
	if !validToken(creds.Access) || !validToken(creds.Refresh) {
		return creds, id, model.Err("BAD_RESPONSE", "登录没有返回完整凭据，未建立账号会话。")
	}
	id, e = decodeIdentity(b)
	return creds, id, e
}
func (c *Client) Me(ctx context.Context, token string) (model.Identity, error) {
	b, _, e := c.request(ctx, "GET", c.api+"/v1/profile/get", nil, accessHeader(token), true)
	if e != nil {
		return model.Identity{}, e
	}
	return decodeIdentity(b)
}
func (c *Client) Refresh(ctx context.Context, old model.Credentials) (model.Credentials, error) {
	if !validToken(old.Refresh) || !validToken(old.Access) {
		return model.Credentials{}, model.Err("UNAUTHORIZED", "登录已失效，请重新连接账号。")
	}
	h := accessHeader(old.Access)
	h["x-jike-refresh-token"] = old.Refresh
	b, headers, e := c.request(ctx, "POST", c.api+"/app_auth_tokens.refresh", nil, h, false)
	if e != nil {
		return model.Credentials{}, e
	}
	creds := decodeCredentials(b, headers, model.Credentials{Refresh: old.Refresh})
	if !validToken(creds.Access) || !validToken(creds.Refresh) {
		return model.Credentials{}, model.Err("BAD_RESPONSE", "平台续期响应不完整。")
	}
	return creds, nil
}
func accessHeader(token string) map[string]string {
	if token == "" {
		return nil
	}
	return map[string]string{"x-jike-access-token": token}
}
func (c *Client) List(ctx context.Context, token, kind, pid, cursor string) (model.Page, error) {
	var out model.Page
	key, e := DecodeCursor(cursor)
	if e != nil {
		return out, e
	}
	body := map[string]any{"limit": 20}
	path := ""
	itemKind := "episode"
	switch kind {
	case "updates":
		path = "/v1/inbox/list"
		body["limit"] = "20"
		if key != nil {
			if e := validateUpdatesCursor(key); e != nil {
				return out, e
			}
		}
	case "subscriptions":
		path = "/v1/subscription/list"
		body["sortOrder"] = "desc"
		body["sortBy"] = "subscribedAt"
		itemKind = "podcast"
	case "favorites":
		path = "/v1/favorite/list"
	case "episodes":
		if !security.ValidID(pid) {
			return out, model.Err("INVALID_ID", "节目 ID 无效。")
		}
		path = "/v1/episode/list"
		body["pid"] = pid
		body["order"] = "desc"
	default:
		return out, model.Err("UNSUPPORTED", "不支持该列表类型。")
	}
	if token == "" {
		if kind == "episodes" && cursor == "" {
			return c.publicEpisodes(ctx, pid)
		}
		return out, model.Err("UNAUTHORIZED", "需要连接账号才能读取云端个人库。")
	}
	if key != nil {
		body["loadMoreKey"] = key
	}
	b, _, e := c.request(ctx, "POST", c.api+path, body, accessHeader(token), true)
	if e != nil {
		return out, e
	}
	var env map[string]json.RawMessage
	if json.Unmarshal(b, &env) != nil {
		return out, model.Err("BAD_RESPONSE", "平台响应不是有效 JSON。")
	}
	items, next, complete, e := DecodePage(env)
	if e != nil {
		return out, e
	}
	if kind == "updates" && next != "" {
		raw, err := DecodeCursor(next)
		if err != nil {
			return out, err
		}
		if err = validateUpdatesCursor(raw); err != nil {
			return out, model.Err("BAD_RESPONSE", "订阅更新分页游标结构无效。")
		}
	}
	// Live-checked 2026-09-12: these two endpoints omit loadMoreKey on
	// terminal pages (including nonempty subscription pages). Keep this
	// contract local; generic/other endpoints still require explicit evidence.
	if kind == "favorites" || kind == "subscriptions" {
		_, hasKey := env["loadMoreKey"]
		_, hasMore := env["hasMore"]
		if !hasKey && !hasMore {
			complete = true
		}
	}
	out = model.Page{Items: make([]model.Item, 0, len(items)), Cursor: next, Complete: complete}
	for _, raw := range items {
		it, e := decodeItem(raw, itemKind)
		if e != nil {
			return model.Page{}, e
		}
		out.Items = append(out.Items, it)
	}
	return out, nil
}
func (c *Client) Detail(ctx context.Context, token, kind, id string) (model.Item, error) {
	if !security.ValidID(id) || (kind != "episode" && kind != "podcast") {
		return model.Item{}, model.Err("INVALID_ID", "节目或单集 ID 无效。")
	}
	if token == "" {
		return c.publicDetail(ctx, kind, id)
	}
	param := "eid"
	if kind == "podcast" {
		param = "pid"
	}
	b, _, e := c.request(ctx, "GET", c.api+"/v1/"+kind+"/get?"+param+"="+id, nil, accessHeader(token), true)
	if e != nil {
		return model.Item{}, e
	}
	var env map[string]json.RawMessage
	if json.Unmarshal(b, &env) != nil {
		return model.Item{}, model.Err("BAD_RESPONSE", "平台响应格式发生变化。")
	}
	return decodeItem(env["data"], kind)
}
func (c *Client) request(ctx context.Context, method, target string, body any, headers map[string]string, retryRead bool) ([]byte, http.Header, error) {
	u, e := url.Parse(target)
	if e != nil {
		return nil, nil, model.Err("NETWORK", "请求地址无效。")
	}
	deviceRequest := strings.HasPrefix(target, c.api+"/")
	if deviceRequest && c.deviceErr != nil {
		return nil, nil, model.Err("INTERNAL", "无法生成客户端会话标识，请重启应用后重试。")
	}
	c.mu.Lock()
	wait := time.Until(c.cooldown[u.Host])
	c.mu.Unlock()
	if wait > 0 {
		return nil, nil, &model.AppError{Code: "RATE_LIMIT", Message: "平台要求稍后重试，当前操作未发送。", RetryAfter: int(wait.Seconds()) + 1}
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, nil, model.Err("CANCELLED", "操作已取消。")
	}
	var payload []byte
	if body != nil {
		payload, e = json.Marshal(body)
		if e != nil {
			return nil, nil, model.Err("INVALID_REQUEST", "请求参数无效。")
		}
	}
	attempts := 1
	if retryRead {
		attempts = 3
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt) * 250 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, nil, model.Err("CANCELLED", "操作已取消。")
			case <-timer.C:
			}
		}
		// Re-check after queued requests and backoff: another in-flight request may have received 429.
		c.mu.Lock()
		remaining := time.Until(c.cooldown[u.Host])
		c.mu.Unlock()
		if remaining > 0 {
			return nil, nil, &model.AppError{Code: "RATE_LIMIT", Message: "平台要求稍后重试，当前操作未发送。", RetryAfter: int(remaining.Seconds()) + 1}
		}
		req, e := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(payload))
		if e != nil {
			return nil, nil, model.Err("NETWORK", "无法创建请求。")
		}
		req.Header.Set("Accept", "application/json, text/html;q=0.8")
		req.Header.Set("User-Agent", userAgent)
		if deviceRequest {
			req.Header.Set("x-jike-device-id", c.deviceID)
		}
		if method == "POST" {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			if !validToken(v) {
				return nil, nil, model.Err("UNAUTHORIZED", "凭据无效，请重新登录。")
			}
			req.Header.Set(k, v)
		}
		resp, e := c.http.Do(req)
		if e != nil {
			if ctx.Err() != nil {
				return nil, nil, model.Err("CANCELLED", "操作已取消。")
			}
			if attempt+1 < attempts {
				continue
			}
			return nil, nil, model.Err("NETWORK", "无法连接平台，请检查网络后重试。")
		}
		const limit = 12 << 20
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		resp.Body.Close()
		if readErr != nil || len(b) > limit {
			return nil, nil, model.Err("BAD_RESPONSE", "平台响应过大或读取失败。")
		}
		switch {
		case resp.StatusCode >= 300 && resp.StatusCode < 400:
			return nil, nil, model.Err("REDIRECT_BLOCKED", "平台返回了未经验证的跳转，已停止请求。")
		case resp.StatusCode == 401:
			if u.Path == "/v1/auth/qrcode/login" {
				var expired struct {
					Code int `json:"code"`
				}
				if json.Unmarshal(b, &expired) == nil && expired.Code == 21 {
					return nil, nil, model.Err("QR_EXPIRED", "二维码已失效，请重新生成。")
				}
			}
			return nil, nil, model.Err("UNAUTHORIZED", "身份验证失败，请重新连接账号。")
		case resp.StatusCode == 403:
			return nil, nil, model.Err("FORBIDDEN", "平台拒绝此操作，可能是权限限制或接口不再支持；不会自动重试。")
		case resp.StatusCode == 429:
			delay := retryAfter(resp.Header.Get("Retry-After"))
			c.mu.Lock()
			c.cooldown[u.Host] = time.Now().Add(time.Duration(delay) * time.Second)
			c.mu.Unlock()
			return nil, nil, &model.AppError{Code: "RATE_LIMIT", Message: "平台限制了请求频率，请稍后再试。", RetryAfter: delay}
		case resp.StatusCode >= 500:
			if attempt+1 < attempts {
				continue
			}
			return nil, nil, model.Err("UPSTREAM", "平台服务暂时不可用。")
		case resp.StatusCode < 200 || resp.StatusCode >= 300:
			failure := &model.AppError{Code: "REQUEST_REJECTED", Message: "平台未接受此请求，可能是接口参数或兼容性问题。"}
			failure.HTTPStatus = resp.StatusCode
			var status struct {
				Code int `json:"code"`
			}
			if json.Unmarshal(b, &status) == nil {
				failure.UpstreamCode = status.Code
			}
			return nil, nil, failure
		}
		if strings.Contains(resp.Header.Get("Content-Type"), "json") || bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) {
			if e = checkBusinessError(b); e != nil {
				return nil, nil, e
			}
		}
		return b, resp.Header, nil
	}
	return nil, nil, model.Err("NETWORK", "请求未完成。")
}
func retryAfter(raw string) int {
	if n, e := strconv.Atoi(raw); e == nil && n >= 0 {
		if n > 86400 {
			return 86400
		}
		if n == 0 {
			return 1
		}
		return n
	}
	if d, e := http.ParseTime(raw); e == nil {
		n := int(time.Until(d).Seconds()) + 1
		if n > 86400 {
			return 86400
		}
		if n > 0 {
			return n
		}
	}
	return 60
}
func checkBusinessError(b []byte) error {
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return model.Err("BAD_RESPONSE", "平台返回无效 JSON。")
	}
	if v, ok := m["error"]; ok && string(v) != "null" && string(v) != `""` {
		return model.Err("REQUEST_REJECTED", "平台返回业务错误；未自动重试。")
	}
	if v, ok := m["success"]; ok && string(v) == "false" {
		return model.Err("REQUEST_REJECTED", "平台未接受此操作。")
	}
	if v, ok := m["code"]; ok {
		var n int
		if json.Unmarshal(v, &n) == nil && n != 0 && n != 200 {
			return model.Err("REQUEST_REJECTED", "平台返回业务错误；未自动重试。")
		}
	}
	return nil
}
