// Package security contains strict URL and credential boundaries.
package security

import (
	"net"
	"net/url"
	"regexp"
	"starling/internal/model"
	"strings"
)

var idPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)
var sharePattern = regexp.MustCompile(`https?://[^\s<>"'，。；！？、（）]+`)

func ValidID(s string) bool { return idPattern.MatchString(s) }

type Share struct {
	Kind string
	ID   string
	URL  string
}

func ParseShare(text string) (Share, error) {
	if len(text) > 16384 {
		return Share{}, model.Err("INVALID_LINK", "分享文本过长。")
	}
	matches := sharePattern.FindAllString(text, -1)
	for _, raw := range matches {
		raw = strings.TrimRight(raw, ".,;!?)］】")
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Host != "www.xiaoyuzhoufm.com" || u.User != nil || u.RawPath != "" {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(parts) != 2 || (parts[0] != "episode" && parts[0] != "podcast") || !ValidID(parts[1]) {
			continue
		}
		return Share{Kind: parts[0], ID: parts[1], URL: "https://www.xiaoyuzhoufm.com/" + parts[0] + "/" + parts[1]}, nil
	}
	return Share{}, model.Err("INVALID_LINK", "请粘贴小宇宙正式的 HTTPS 节目或单集链接，不支持短链。")
}

// Media hosts are deliberately narrow. Unknown legitimate CDNs require review, not automatic trust.
func ValidateMediaURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 8192 || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return model.Err("UNSUPPORTED_MEDIA", "媒体地址不符合安全规则。")
	}
	switch strings.ToLower(u.Hostname()) {
	case "media.xyzcdn.net", "media.xiaoyuzhoufm.com":
		return nil
	}
	return model.Err("UNSUPPORTED_MEDIA", "该音频域名尚未列入受信任来源，请在官方页面收听。")
}
func ValidateImageURL(raw string) string {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	switch h {
	case "image.xyzcdn.net", "bts-image.xyzcdn.net", "media.xyzcdn.net":
		return raw
	}
	return ""
}
func ValidateExternalURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 8192 || u.Scheme != "https" || u.User != nil || u.Port() != "" || strings.ContainsAny(raw, "\x00\r\n") {
		return model.Err("INVALID_LINK", "只能打开普通 HTTPS 外部链接。")
	}
	h := strings.ToLower(u.Hostname())
	if h == "" || h == "localhost" || !strings.Contains(h, ".") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".localhost") {
		return model.Err("INVALID_LINK", "不允许打开本地网络地址。")
	}
	if ip := net.ParseIP(h); ip != nil {
		return model.Err("INVALID_LINK", "不允许打开 IP 地址链接。")
	}
	return nil
}
func PublicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast()
}
