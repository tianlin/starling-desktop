package app

import (
	"context"
	"math"
	"starling/internal/model"
	"starling/internal/security"
	"strings"
)

func cleanItem(it model.Item) (model.Item, error) {
	if !security.ValidID(it.ID) || (it.Kind != "episode" && it.Kind != "podcast") {
		return model.Item{}, model.Err("INVALID_ID", "内容 ID 无效。")
	}
	it.MediaURL = ""
	it.ShowNotes = ""
	if len(it.Description) > 2000 {
		it.Description = string([]rune(it.Description)[:min(len([]rune(it.Description)), 500)])
	}
	if len(it.Title) > 2000 {
		it.Title = string([]rune(it.Title)[:min(len([]rune(it.Title)), 500)])
	}
	it.SourceURL = "https://www.xiaoyuzhoufm.com/" + it.Kind + "/" + it.ID
	it.Image = security.ValidateImageURL(it.Image)
	if math.IsNaN(it.Duration) || math.IsInf(it.Duration, 0) || it.Duration < 0 || it.Duration > 7*86400 {
		it.Duration = 0
	}
	return it, nil
}
func (s *Service) Queue(epoch uint64, op string, it model.Item) ([]model.Item, error) {
	return s.localList(epoch, "queue", op, it)
}
func (s *Service) Bookmarks(epoch uint64, op string, it model.Item) ([]model.Item, error) {
	return s.localList(epoch, "bookmarks", op, it)
}
func (s *Service) localList(epoch uint64, kind, op string, it model.Item) ([]model.Item, error) {
	var out = []model.Item{}
	e := s.session.Commit(epoch, func(scope string) error {
		if _, e := s.store.Get(scope, kind, "main", &out); e != nil {
			return e
		}
		if out == nil {
			out = []model.Item{}
		}
		if op == "read" {
			return nil
		}
		if op == "clear" {
			out = []model.Item{}
			return s.store.Put(scope, kind, "main", out)
		}
		if op != "append" && op != "next" && op != "remove" {
			return model.Err("UNSUPPORTED", "不支持的本地列表操作。")
		}
		if kind == "queue" && it.Kind != "episode" {
			return model.Err("INVALID_ITEM", "只有单集可以加入稍后听。")
		}
		var e error
		it, e = cleanItem(it)
		if e != nil {
			return e
		}
		filtered := make([]model.Item, 0, len(out))
		found := false
		for _, v := range out {
			if v.Key() != it.Key() {
				filtered = append(filtered, v)
			} else {
				found = true
			}
		}
		switch op {
		case "append":
			if !found {
				if len(out) >= 2000 {
					return model.Err("LIMIT", "本地列表最多保存 2000 条。")
				}
				out = append(out, it)
			}
		case "next":
			if !found && len(out) >= 2000 {
				return model.Err("LIMIT", "本地列表最多保存 2000 条。")
			}
			out = append([]model.Item{it}, filtered...)
		case "remove":
			out = filtered
		}
		return s.store.Put(scope, kind, "main", out)
	})
	return out, e
}
func (s *Service) SaveProgress(epoch uint64, p model.Progress) error {
	if p.Item.Kind != "episode" || math.IsNaN(p.Position) || math.IsInf(p.Position, 0) || p.Position < 0 || p.Position > 7*86400 || math.IsNaN(p.Duration) || math.IsInf(p.Duration, 0) || p.Duration < 0 || p.Duration > 7*86400 || (p.Duration > 0 && p.Position > p.Duration+1) {
		return model.Err("INVALID_PROGRESS", "播放位置或时长无效。")
	}
	it, e := cleanItem(p.Item)
	if e != nil {
		return e
	}
	p.Item = it
	p.UpdatedAt = model.Now()
	return s.session.Commit(epoch, func(scope string) error { return s.store.Put(scope, "progress", p.Item.ID, p) })
}
func (s *Service) Detail(ctx context.Context, epoch uint64, kind, id string) (model.Item, error) {
	if snap := s.session.Snapshot(); snap.Epoch != epoch {
		return model.Item{}, model.ErrStale
	}
	var it model.Item
	var e error
	if s.session.View().State == "connected" {
		_, e = s.session.DoAt(ctx, epoch, func(c context.Context, token string) error {
			var er error
			it, er = s.p.Detail(c, token, kind, id)
			return er
		})
	} else {
		_, e = s.session.Guest(ctx, func(c context.Context) error { var er error; it, er = s.p.Detail(c, "", kind, id); return er })
	}
	if e != nil {
		return model.Item{}, e
	}
	if it.ID != id || it.Kind != kind {
		return model.Item{}, model.Err("BAD_RESPONSE", "平台返回了不同的内容 ID。")
	}
	if e := s.session.Commit(epoch, func(string) error { return nil }); e != nil {
		return model.Item{}, e
	}
	return it, nil
}
func (s *Service) OpenLink(ctx context.Context, epoch uint64, text string) (model.Item, error) {
	share, e := security.ParseShare(text)
	if e != nil {
		return model.Item{}, e
	}
	return s.Detail(ctx, epoch, share.Kind, share.ID)
}
func (s *Service) Resolve(ctx context.Context, epoch uint64, id, requestID string) (model.Playback, error) {
	return s.resolve(ctx, epoch, 0, id, requestID, false)
}

// ResolveAt accepts only a strictly newer selection within the session epoch.
// Wails dispatches bridge calls concurrently, so arrival order is not intent order.
func (s *Service) ResolveAt(ctx context.Context, epoch, generation uint64, id, requestID string) (model.Playback, error) {
	return s.resolve(ctx, epoch, generation, id, requestID, true)
}

const maxPlaybackGeneration uint64 = 1<<53 - 1 // Exact positive integers in the JS bridge.

func validatePlaybackRequest(requestID string, generation uint64, ordered bool) error {
	if requestID == "" || len(requestID) > 128 || strings.ContainsAny(requestID, "\r\n\x00") {
		return model.Err("INVALID_REQUEST", "播放请求标识无效。")
	}
	if ordered && (generation == 0 || generation > maxPlaybackGeneration) {
		return model.Err("INVALID_REQUEST", "播放请求代次无效。")
	}
	return nil
}

// Caller holds s.mu inside session.Commit, preserving the session -> service lock order.
func (s *Service) resetPlaybackEpochLocked(epoch uint64) {
	if s.playEpoch == epoch {
		return
	}
	if s.playCancel != nil {
		s.playCancel()
	}
	s.playEpoch = epoch
	s.playGeneration = 0
	s.playOrdered = false
	s.playID = ""
	s.playCancel = nil
}

func (s *Service) resolve(ctx context.Context, epoch, generation uint64, id, requestID string, ordered bool) (model.Playback, error) {
	var out model.Playback
	if e := validatePlaybackRequest(requestID, generation, ordered); e != nil {
		return out, e
	}
	if !security.ValidID(id) {
		return out, model.Err("INVALID_ID", "单集 ID 无效。")
	}
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	e := s.session.Commit(epoch, func(string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.resetPlaybackEpochLocked(epoch)
		if requestCtx.Err() != nil {
			return model.Err("CANCELLED", "播放请求已取消。")
		}
		if !ordered {
			// Once this epoch uses client ordering, late legacy calls cannot bypass it.
			if s.playOrdered || s.playGeneration == maxPlaybackGeneration {
				return model.Err("INVALID_REQUEST", "请刷新界面后使用带代次的播放请求。")
			}
			generation = s.playGeneration + 1
		} else if generation <= s.playGeneration {
			return model.Err("CANCELLED", "播放请求已被取消或更新的选择替代。")
		}
		s.playGeneration = generation
		s.playOrdered = ordered
		if s.playCancel != nil {
			s.playCancel()
		}
		s.playCancel = cancel
		s.playID = requestID
		return nil
	})
	if e != nil {
		return out, e
	}
	it, e := s.Detail(requestCtx, epoch, "episode", id)
	if e != nil {
		if requestCtx.Err() == context.Canceled && !model.IsCode(e, "STALE_SESSION") {
			return out, model.Err("CANCELLED", "播放请求已取消。")
		}
		return out, e
	}
	if it.Restricted {
		return out, model.Err("RESTRICTED", it.Restriction)
	}
	if e = security.ValidateMediaURL(it.MediaURL); e != nil {
		return out, e
	}
	out = model.Playback{Item: it, URL: it.MediaURL, Epoch: epoch}
	e = s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.playID != requestID || s.playGeneration != generation || requestCtx.Err() != nil {
			return model.Err("CANCELLED", "播放请求已取消。")
		}
		var p model.Progress
		if _, e := s.store.Get(scope, "progress", id, &p); e != nil {
			return e
		}
		if !p.Ended {
			out.Position = p.Position
		}
		return nil
	})
	return out, e
}
func (s *Service) CancelResolve(epoch uint64, requestID string) error {
	return s.cancelResolve(epoch, 0, requestID, false)
}

// CancelResolveAt also fences a queued request that has not entered ResolveAt yet.
func (s *Service) CancelResolveAt(epoch, generation uint64, requestID string) error {
	return s.cancelResolve(epoch, generation, requestID, true)
}

func (s *Service) cancelResolve(epoch, generation uint64, requestID string, ordered bool) error {
	if e := validatePlaybackRequest(requestID, generation, ordered); e != nil {
		return e
	}
	return s.session.Commit(epoch, func(string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.resetPlaybackEpochLocked(epoch)
		if !ordered {
			if s.playOrdered {
				return model.Err("INVALID_REQUEST", "请刷新界面后使用带代次的播放请求。")
			}
			if s.playID != requestID {
				return nil
			}
		} else {
			if generation < s.playGeneration || (generation == s.playGeneration && s.playID != requestID) {
				return nil
			}
			s.playGeneration = generation
			s.playOrdered = true
		}
		if s.playCancel != nil {
			s.playCancel()
		}
		s.playID = ""
		s.playCancel = nil
		return nil
	})
}
