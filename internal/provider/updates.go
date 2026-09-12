package provider

import (
	"encoding/json"
	"starling/internal/model"
	"starling/internal/security"
	"time"
)

// Forward the provider's object intact; display sorting cannot reconstruct it.
func validateUpdatesCursor(raw json.RawMessage) error {
	var key struct {
		ID      string `json:"id"`
		PubDate string `json:"pubDate"`
	}
	if json.Unmarshal(raw, &key) != nil || !security.ValidID(key.ID) {
		return model.Err("BAD_CURSOR", "订阅更新分页游标无效。")
	}
	if _, e := time.Parse(time.RFC3339Nano, key.PubDate); e != nil {
		return model.Err("BAD_CURSOR", "订阅更新分页时间无效。")
	}
	return nil
}
