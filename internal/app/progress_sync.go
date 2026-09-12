package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math"
	"starling/internal/model"
	"starling/internal/provider"
	"starling/internal/security"
	"time"
)

type ProgressSyncStatus struct {
	State       string `json:"state"`
	LastSuccess string `json:"lastSuccess,omitempty"`
	Message     string `json:"message,omitempty"`
	Pending     int    `json:"pending"`
}
type ProgressConflict struct {
	LocalPosition float64 `json:"localPosition"`
	CloudPosition float64 `json:"cloudPosition"`
	Token         string  `json:"token"`
}
type PreparedProgress struct {
	Position float64            `json:"position"`
	Sync     ProgressSyncStatus `json:"sync"`
	Conflict *ProgressConflict  `json:"conflict,omitempty"`
}

// Local is authoritative alongside its outbox in a single durable row. The
// older progress kind is a compatibility projection, never a migration source
// for uploads.
type progressRecord struct {
	ReadyRevision uint64
	Local         model.Progress
	Revision      uint64
	Baseline      *model.CloudProgress
	Pending       *model.CloudProgress
	Conflict      *model.CloudProgress
	ConflictToken string
}

func cloudEqual(a, b *model.CloudProgress) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ta, ea := time.Parse(time.RFC3339Nano, a.PlayedAt)
	tb, eb := time.Parse(time.RFC3339Nano, b.PlayedAt)
	return a.EpisodeID == b.EpisodeID && math.Floor(a.Position) == math.Floor(b.Position) && ((ea == nil && eb == nil && ta.UnixMilli() == tb.UnixMilli()) || a.PlayedAt == b.PlayedAt)
}
func cloudOf(p model.Progress) *model.CloudProgress {
	return &model.CloudProgress{EpisodeID: p.Item.ID, PodcastID: p.Item.PodcastID, Position: p.Position, PlayedAt: p.UpdatedAt}
}
func (s *Service) loadProgress(scope, id string) (progressRecord, error) {
	var r progressRecord
	ok, e := s.store.Get(scope, "progress-sync", id, &r)
	if e != nil {
		return r, e
	}
	if !ok {
		_, e = s.store.Get(scope, "progress", id, &r.Local)
	}
	return r, e
}
func (s *Service) putProgress(scope, id string, r progressRecord) error {
	if e := s.store.Put(scope, "progress-sync", id, r); e != nil {
		return e
	}
	if r.Local.Item.ID != "" {
		return s.store.Put(scope, "progress", id, r.Local)
	}
	return nil
}
func (s *Service) initProgress() {
	s.progressCtx, s.progressStop = context.WithCancel(context.Background())
	s.progressWake = make(chan struct{}, 1)
	s.progressGate = make(chan struct{}, 1)
	if _, ok := s.p.(provider.ProgressProvider); !ok {
		return
	}
	s.progressJobs.Add(1)
	go func() {
		defer s.progressJobs.Done()
		tick := time.NewTicker(15 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-s.progressCtx.Done():
				return
			case <-tick.C:
			case <-s.progressWake:
			}
			v := s.Session()
			s.mu.Lock()
			backoff := s.progressStatusEpoch == v.Epoch && time.Now().Before(s.progressRetryAt)
			s.mu.Unlock()
			if backoff {
				continue
			}
			if v.State == "connected" {
				status, err := s.ProgressStatus(v.Epoch)
				if err != nil || status.Pending == 0 {
					continue
				}
				ctx, cancel := context.WithTimeout(s.progressCtx, 10*time.Second)
				s.ProgressRetry(ctx, v.Epoch)
				cancel()
			}
		}
	}()
}
func (s *Service) wakeProgress() {
	select {
	case s.progressWake <- struct{}{}:
	default:
	}
}
func (s *Service) ProgressStatus(epoch uint64) (ProgressSyncStatus, error) {
	out := ProgressSyncStatus{State: "idle"}
	e := s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.settings.ProgressSyncDisabled || scope == "guest" {
			out.State = "disabled"
			return nil
		}
		if _, ok := s.p.(provider.ProgressProvider); !ok {
			out.State = "disabled"
			return nil
		}
		rows, e := s.store.List(scope, "progress-sync")
		if e != nil {
			return e
		}
		conflict := false
		for _, raw := range rows {
			var r progressRecord
			if e = json.Unmarshal(raw, &r); e != nil {
				return e
			}
			if r.Pending != nil {
				out.Pending++
			}
			conflict = conflict || r.Conflict != nil
		}
		if s.progressStatusEpoch == epoch {
			out.LastSuccess = s.progressLastSuccess
			out.Message = s.progressMessage
		}
		if out.LastSuccess != "" {
			out.State = "synced"
		}
		if out.Pending > 0 {
			out.State = "pending"
		}
		if out.Message != "" {
			out.State = "error"
		}
		if s.progressBusy && s.progressStatusEpoch == epoch {
			out.State = "syncing"
		}
		if conflict {
			out.State = "conflict"
		}
		return nil
	})
	return out, e
}
func (s *Service) progressOperation(ctx context.Context, epoch uint64) (context.Context, func(), error) {
	select {
	case s.progressGate <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	c, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.progressCtx, cancel)
	e := s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.settings.ProgressSyncDisabled || scope == "guest" {
			return model.Err("SYNC_DISABLED", "进度同步已关闭。")
		}
		if c.Err() != nil {
			return c.Err()
		}
		s.progressCancel = cancel
		s.progressBusy = true
		s.progressStatusEpoch = epoch
		return nil
	})
	if e != nil {
		stop()
		cancel()
		<-s.progressGate
		return nil, nil, e
	}
	return c, func() {
		stop()
		cancel()
		s.mu.Lock()
		s.progressCancel = nil
		s.progressBusy = false
		s.mu.Unlock()
		<-s.progressGate
	}, nil
}
func (s *Service) progressResult(epoch uint64, e error) {
	_ = s.session.Commit(epoch, func(string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.progressStatusEpoch = epoch
		if e != nil {
			s.progressMessage = "云端进度暂未同步，可重试。"
			s.progressFailures = min(s.progressFailures+1, 8)
			delay := time.Duration(1<<s.progressFailures) * time.Second
			if retry := model.PublicError(e).RetryAfter; retry > 0 {
				delay = max(delay, time.Duration(retry)*time.Second)
			}
			s.progressRetryAt = time.Now().Add(delay)
		} else {
			s.progressFailures = 0
			s.progressRetryAt = time.Time{}
			s.progressMessage = ""
			s.progressLastSuccess = model.Now()
		}
		return nil
	})
}
func (s *Service) readCloud(ctx context.Context, epoch uint64, id string) (*model.CloudProgress, error) {
	p, ok := s.p.(provider.ProgressProvider)
	if !ok {
		return nil, model.Err("SYNC_DISABLED", "当前接入不支持进度同步。")
	}
	var rows []model.CloudProgress
	_, e := s.session.DoAt(ctx, epoch, func(c context.Context, t string) error {
		var er error
		rows, er = p.ReadProgress(c, t, []string{id})
		return er
	})
	if e != nil {
		return nil, e
	}
	for _, r := range rows {
		if r.EpisodeID == id {
			return &r, nil
		}
	}
	return nil, nil
}

// A cloud change and local action within the timestamp uncertainty window need
// a choice; seek backwards is an ordinary newer action, never max(position).
func reconcile(r *progressRecord, cloud *model.CloudProgress) {
	if r.Pending == nil {
		r.Baseline = cloud
		r.Conflict = nil
		r.ConflictToken = ""
		applyCloud(r, cloud)
		return
	}
	if cloudEqual(r.Pending, cloud) {
		r.Baseline = cloud
		r.Pending = nil
		r.Conflict = nil
		r.ConflictToken = ""
		return
	}
	if cloudEqual(r.Baseline, cloud) {
		return
	}
	if cloud == nil {
		return
	}
	lt, le := time.Parse(time.RFC3339Nano, r.Pending.PlayedAt)
	ct, ce := time.Parse(time.RFC3339Nano, cloud.PlayedAt)
	reliable := le == nil && ce == nil && lt.Before(time.Now().Add(5*time.Minute)) && ct.Before(time.Now().Add(5*time.Minute))
	if reliable && lt.Sub(ct) > 5*time.Second {
		r.Baseline = cloud
		return
	}
	if reliable && ct.Sub(lt) > 5*time.Second {
		r.Pending = nil
		r.Baseline = cloud
		r.Conflict = nil
		r.ConflictToken = ""
		applyCloud(r, cloud)
		return
	}
	r.Conflict = cloud
	var b [24]byte
	_, _ = rand.Read(b[:])
	r.ConflictToken = hex.EncodeToString(b[:])
}
func applyCloud(r *progressRecord, c *model.CloudProgress) {
	if c == nil {
		return
	}
	ended := r.Local.Ended && math.Floor(r.Local.Position) == math.Floor(c.Position)
	r.Local.Position = c.Position
	if r.Local.Duration > 0 {
		r.Local.Position = min(r.Local.Position, r.Local.Duration)
	}
	r.Local.UpdatedAt = c.PlayedAt
	r.Local.Ended = ended
	if r.Local.Item.ID == "" {
		r.Local.Item = model.Item{ID: c.EpisodeID, Kind: "episode", PodcastID: c.PodcastID}
	}
}
func (s *Service) progressView(epoch uint64, id string) (PreparedProgress, error) {
	var out PreparedProgress
	e := s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		r, e := s.loadProgress(scope, id)
		if e != nil {
			return e
		}
		if !r.Local.Ended {
			out.Position = r.Local.Position
		}
		if r.Conflict != nil {
			out.Conflict = &ProgressConflict{LocalPosition: r.Local.Position, CloudPosition: r.Conflict.Position, Token: r.ConflictToken}
		}
		return nil
	})
	if e != nil {
		return out, e
	}
	out.Sync, e = s.ProgressStatus(epoch)
	return out, e
}
func (s *Service) PrepareProgress(ctx context.Context, epoch uint64, id string) (PreparedProgress, error) {
	if !security.ValidID(id) {
		return PreparedProgress{}, model.Err("INVALID_ID", "单集 ID 无效。")
	}
	status, e := s.ProgressStatus(epoch)
	if e != nil {
		return PreparedProgress{}, e
	}
	if status.State == "disabled" {
		return s.progressView(epoch, id)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	c, done, e := s.progressOperation(ctx, epoch)
	if e != nil {
		if model.IsCode(e, "STALE_SESSION") {
			return PreparedProgress{}, e
		}
		s.progressResult(epoch, e)
		return s.progressView(epoch, id)
	}
	e = s.reconcileProgress(c, epoch, id)
	done()
	s.progressResult(epoch, e)
	if model.IsCode(e, "STALE_SESSION") {
		return PreparedProgress{}, e
	}
	return s.progressView(epoch, id)
}
func (s *Service) reconcileProgress(ctx context.Context, epoch uint64, id string) error {
	var before progressRecord
	e := s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		var er error
		before, er = s.loadProgress(scope, id)
		return er
	})
	if e != nil {
		return e
	}
	cloud, e := s.readCloud(ctx, epoch, id)
	if e != nil {
		return e
	}
	var metadata model.Item
	if before.Local.Item.Title == "" && cloud != nil {
		metadata, e = s.Detail(ctx, epoch, "episode", id)
		if e != nil {
			return e
		}
		metadata, e = cleanItem(metadata)
		if e != nil {
			return e
		}
	}
	return s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.settings.ProgressSyncDisabled || ctx.Err() != nil {
			return context.Canceled
		}
		r, e := s.loadProgress(scope, id)
		if e != nil {
			return e
		}
		if r.Revision != before.Revision {
			return nil
		}
		if metadata.ID != "" {
			r.Local.Item = metadata
			r.Local.Duration = metadata.Duration
		}
		reconcile(&r, cloud)
		r.ReadyRevision = r.Revision
		return s.putProgress(scope, id, r)
	})
}
func (s *Service) ProgressRetry(ctx context.Context, epoch uint64) (ProgressSyncStatus, error) {
	status, e := s.ProgressStatus(epoch)
	if e != nil || status.State == "disabled" || status.Pending == 0 {
		return status, e
	}
	c, done, e := s.progressOperation(ctx, epoch)
	if e != nil {
		return status, e
	}
	performed := false
	e = s.flushProgress(c, epoch, &performed)
	done()
	if performed || e != nil {
		s.progressResult(epoch, e)
	}
	status, se := s.ProgressStatus(epoch)
	if model.IsCode(e, "STALE_SESSION") {
		return status, e
	}
	return status, se
}
func (s *Service) flushProgress(ctx context.Context, epoch uint64, performed *bool) error {
	var ids []string
	e := s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		rows, e := s.store.List(scope, "progress-sync")
		if e != nil {
			return e
		}
		for _, raw := range rows {
			var r progressRecord
			if e = json.Unmarshal(raw, &r); e != nil {
				return e
			}
			if r.Pending != nil && r.Conflict == nil {
				ids = append(ids, r.Local.Item.ID)
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	for _, id := range ids {
		*performed = true
		if e = s.reconcileProgress(ctx, epoch, id); e != nil {
			return e
		}
		var sent *model.CloudProgress
		var rev uint64
		e = s.session.Commit(epoch, func(scope string) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.settings.ProgressSyncDisabled || ctx.Err() != nil {
				return context.Canceled
			}
			r, e := s.loadProgress(scope, id)
			if e != nil {
				return e
			}
			if r.Conflict == nil && r.ReadyRevision == r.Revision {
				sent = r.Pending
				rev = r.Revision
			}
			return nil
		})
		if e != nil {
			return e
		}
		if sent == nil {
			continue
		}
		p := s.p.(provider.ProgressProvider)
		_, e = s.session.DoOnceAt(ctx, epoch, func(c context.Context, t string) error { return p.WriteProgress(c, t, []model.CloudProgress{*sent}) })
		if e != nil {
			return e
		}
		e = s.session.Commit(epoch, func(scope string) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.settings.ProgressSyncDisabled || ctx.Err() != nil {
				return context.Canceled
			}
			r, e := s.loadProgress(scope, id)
			if e != nil {
				return e
			}
			r.Baseline = sent
			if r.Revision == rev {
				r.Pending = nil
				r.Conflict = nil
				r.ConflictToken = ""
			}
			return s.putProgress(scope, id, r)
		})
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) ChooseProgress(ctx context.Context, epoch uint64, id, token, choice string) (PreparedProgress, error) {
	if !security.ValidID(id) || (choice != "local" && choice != "cloud") {
		return PreparedProgress{}, model.Err("INVALID_REQUEST", "进度选择无效。")
	}
	e := s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.settings.ProgressSyncDisabled {
			return model.Err("SYNC_DISABLED", "进度同步已关闭。")
		}
		r, e := s.loadProgress(scope, id)
		if e != nil {
			return e
		}
		if token == "" || r.Conflict == nil || r.ConflictToken != token {
			return model.Err("STALE_CONFLICT", "进度已变化，请重新选择。")
		}
		r.Revision++
		r.Baseline = r.Conflict
		if choice == "cloud" {
			applyCloud(&r, r.Conflict)
			r.Pending = nil
		} else {
			r.Local.UpdatedAt = model.Now()
			r.Pending = cloudOf(r.Local)
		}
		r.Conflict = nil
		r.ConflictToken = ""
		return s.putProgress(scope, id, r)
	})
	if e != nil {
		return PreparedProgress{}, e
	}
	if choice == "local" {
		s.wakeProgress()
	}
	return s.progressView(epoch, id)
}
func (s *Service) FlushProgress(ctx context.Context, epoch uint64) (ProgressSyncStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.ProgressRetry(ctx, epoch)
}
