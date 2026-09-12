package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"starling/internal/model"
	"starling/internal/security"
	"time"
)

// ProgressProvider is optional: providers without authenticated playback sync
// need not implement it. Session refresh belongs to the caller.
type ProgressProvider interface {
	ReadProgress(context.Context, string, []string) ([]model.CloudProgress, error)
	WriteProgress(context.Context, string, []model.CloudProgress) error
}

func validCloudProgress(row model.CloudProgress) bool {
	if !security.ValidID(row.EpisodeID) || !security.ValidID(row.PodcastID) || math.IsNaN(row.Position) || math.IsInf(row.Position, 0) || row.Position < 0 || row.Position > 7*24*60*60 {
		return false
	}
	stamp, err := time.Parse(time.RFC3339Nano, row.PlayedAt)
	return err == nil && !stamp.IsZero()
}
func progressBadResponse() error {
	return model.Err("BAD_RESPONSE", "平台播放进度响应格式无效。")
}
func (c *Client) ReadProgress(ctx context.Context, token string, ids []string) ([]model.CloudProgress, error) {
	if !validToken(token) {
		return nil, model.Err("UNAUTHORIZED", "请先连接账号。")
	}
	requested := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !security.ValidID(id) || requested[id] {
			return nil, model.Err("INVALID_ID", "单集 ID 无效或重复。")
		}
		requested[id] = true
	}
	if len(ids) == 0 {
		return []model.CloudProgress{}, nil
	}
	b, _, err := c.request(ctx, "POST", c.api+"/v1/playback-progress/list", map[string]any{"eids": ids}, accessHeader(token), true)
	if err != nil {
		return nil, err
	}
	var env map[string]json.RawMessage
	if json.Unmarshal(b, &env) != nil || env == nil {
		return nil, progressBadResponse()
	}
	raw := bytes.TrimSpace(env["data"])
	if len(raw) == 0 || raw[0] != '[' {
		return nil, progressBadResponse()
	}
	// Pointers distinguish an explicit zero from missing/null progress.
	var records []struct {
		EpisodeID string   `json:"eid"`
		PodcastID string   `json:"pid"`
		Position  *float64 `json:"progress"`
		PlayedAt  string   `json:"playedAt"`
	}
	if json.Unmarshal(raw, &records) != nil {
		return nil, progressBadResponse()
	}
	out := make([]model.CloudProgress, 0, len(records))
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		if record.Position == nil {
			return nil, progressBadResponse()
		}
		row := model.CloudProgress{EpisodeID: record.EpisodeID, PodcastID: record.PodcastID, Position: *record.Position, PlayedAt: record.PlayedAt}
		if !validCloudProgress(row) || !requested[row.EpisodeID] || seen[row.EpisodeID] {
			return nil, progressBadResponse()
		}
		seen[row.EpisodeID] = true
		out = append(out, row)
	}
	return out, nil
}
func (c *Client) WriteProgress(ctx context.Context, token string, rows []model.CloudProgress) error {
	if !validToken(token) {
		return model.Err("UNAUTHORIZED", "请先连接账号。")
	}
	data := make([]model.CloudProgress, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if !validCloudProgress(row) || seen[row.EpisodeID] {
			return model.Err("INVALID_REQUEST", "播放进度参数无效或重复。")
		}
		seen[row.EpisodeID] = true
		row.Position = math.Floor(row.Position)
		data = append(data, row)
	}
	if len(data) == 0 {
		return nil
	}
	headers := accessHeader(token)
	headers["Local-Time"] = time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format(time.RFC3339)
	headers["Timezone"] = "Asia/Shanghai"
	// A transport failure can follow an applied mutation: never retry here.
	b, _, err := c.request(ctx, "POST", c.api+"/v1/playback-progress/update", map[string]any{"data": data}, headers, false)
	if err != nil {
		return err
	}
	var env map[string]json.RawMessage
	if json.Unmarshal(b, &env) != nil || env == nil {
		return progressBadResponse()
	}
	// HTTP acceptance alone is not a readback confirmation of the saved record.
	return nil
}
