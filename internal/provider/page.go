package provider

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"starling/internal/model"
)

func EncodeCursor(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}
func DecodeCursor(s string) (json.RawMessage, error) {
	if s == "" {
		return nil, nil
	}
	if len(s) > 8192 {
		return nil, model.Err("BAD_CURSOR", "分页游标过长。")
	}
	b, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil || !json.Valid(b) {
		return nil, model.Err("BAD_CURSOR", "分页游标无效。")
	}
	var v any
	json.Unmarshal(b, &v)
	switch v.(type) {
	case map[string]any, string, float64:
	default:
		return nil, model.Err("BAD_CURSOR", "分页游标类型无效。")
	}
	return b, nil
}

// Absence of pagination metadata is NOT proof that the entire library was returned.
func DecodePage(env map[string]json.RawMessage) ([]json.RawMessage, string, bool, error) {
	raw, ok := env["data"]
	if !ok || len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, "", false, model.Err("BAD_RESPONSE", "平台列表结构不符合预期。")
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil {
		return nil, "", false, model.Err("BAD_RESPONSE", "平台列表结构不符合预期。")
	}
	var more *bool
	if r, ok := env["hasMore"]; ok {
		if json.Unmarshal(r, &more) != nil || more == nil {
			return nil, "", false, model.Err("BAD_RESPONSE", "分页标志无效。")
		}
	}
	r, hasKey := env["loadMoreKey"]
	keyEmpty := hasKey && (bytes.Equal(bytes.TrimSpace(r), []byte("null")) || bytes.Equal(bytes.TrimSpace(r), []byte(`""`)))
	cursor := ""
	if hasKey && !keyEmpty {
		cursor = EncodeCursor(r)
		if cursor == "" {
			return nil, "", false, model.Err("BAD_RESPONSE", "分页游标无效。")
		}
		if _, e := DecodeCursor(cursor); e != nil {
			return nil, "", false, e
		}
	}
	complete := keyEmpty || (more != nil && !*more)
	if more != nil && ((*more && cursor == "") || (!*more && cursor != "")) {
		return nil, "", false, model.Err("PAGINATION", "平台分页标志相互矛盾。")
	}
	if len(items) == 0 && cursor != "" {
		return nil, "", false, model.Err("PAGINATION", "平台返回空页但仍要求继续，已停止加载。")
	}
	return items, cursor, complete, nil
}
