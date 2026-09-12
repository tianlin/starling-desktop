// Package app owns the allowlisted business operations shared by desktop and tests.
package app

import (
	"context"
	"encoding/json"
	"sort"
	"starling/internal/model"
	"starling/internal/provider"
	"starling/internal/security"
	"starling/internal/session"
	"starling/internal/store"
	"sync"
	"time"
)

const Version = "0.4.0"

type Service struct {
	progressCtx           context.Context
	progressStop          context.CancelFunc
	progressCancel        context.CancelFunc
	progressWake          chan struct{}
	progressGate          chan struct{}
	progressJobs          sync.WaitGroup
	progressBusy          bool
	progressStatusEpoch   uint64
	progressLastSuccess   string
	progressMessage       string
	progressRetryAt       time.Time
	progressFailures      int
	p                     provider.Provider
	store                 *store.Store
	session               *session.Manager
	mu                    sync.Mutex
	settings              model.Settings
	stages                map[string]*libraryStage
	revision              uint64
	smsUntil              time.Time
	playID                string
	playCancel            context.CancelFunc
	playEpoch             uint64
	playGeneration        uint64
	playOrdered           bool
	log                   []diagnosticEvent
	startupError          error
	commentEpoch          uint64
	commentAttempts       map[string]struct{}
	commentBusy           bool
	commentReadEpoch      uint64
	commentTargets        map[string]map[string]string
	searchEpoch           uint64
	searchGeneration      uint64
	subscriptionEpoch     uint64
	subscriptionAttempts  map[string]bool
	subscriptionBusy      map[string]bool
	subscriptionPending   map[string]bool
	subscriptionConfirmed map[string]model.Item
}
type Bootstrap struct {
	Version             string            `json:"version"`
	Adapter             string            `json:"adapter"`
	Session             model.SessionView `json:"session"`
	Settings            model.Settings    `json:"settings"`
	Queue               []model.Item      `json:"queue"`
	Bookmarks           []model.Item      `json:"bookmarks"`
	History             []model.Progress  `json:"history"`
	Warning             *model.AppError   `json:"warning,omitempty"`
	PlaybackGeneration  uint64            `json:"playbackGeneration"`
	DiscoveryGeneration uint64            `json:"discoveryGeneration"`
}

func New(p provider.Provider, db *store.Store, v security.Vault) *Service {
	s := &Service{p: p, store: db, session: session.New(p, v), settings: model.DefaultSettings(), stages: map[string]*libraryStage{}}
	var settings model.Settings
	if ok, e := db.Get("global", "settings", "main", &settings); e != nil {
		s.startupError = e
	} else if ok {
		if e = settings.Validate(); e != nil {
			s.startupError = e
		} else {
			s.settings = settings
		}
	}
	s.initProgress()
	return s
}
func (s *Service) Close() {
	s.progressStop()
	s.progressJobs.Wait()
	s.session.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stage := range s.stages {
		if stage.cancel != nil {
			stage.cancel()
		}
	}
	if s.playCancel != nil {
		s.playCancel()
	}
}
func (s *Service) Settings() model.Settings   { s.mu.Lock(); defer s.mu.Unlock(); return s.settings }
func (s *Service) Session() model.SessionView { return s.session.View() }
func (s *Service) SaveSettings(v model.Settings) error {
	if e := v.Validate(); e != nil {
		return e
	}
	view := s.session.View()
	if !v.ExperimentalAccount && view.Identity != nil {
		return model.Err("ACCOUNT_ACTIVE", "关闭账号接入前，请先退出账号。")
	}
	return s.session.Commit(view.Epoch, func(string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if e := s.store.Put("global", "settings", "main", v); e != nil {
			return e
		}
		if v.ProgressSyncDisabled && s.progressCancel != nil {
			s.progressCancel()
		}
		s.settings = v
		s.startupError = nil
		return nil
	})
}
func (s *Service) Bootstrap() (Bootstrap, error) {
	b := Bootstrap{Version: Version, Adapter: provider.AdapterVersion, Session: s.session.View(), Settings: s.Settings(), Queue: []model.Item{}, Bookmarks: []model.Item{}, History: []model.Progress{}}
	s.mu.Lock()
	if s.startupError != nil {
		b.Warning = model.PublicError(s.startupError)
	}
	s.mu.Unlock()
	e := s.session.Commit(b.Session.Epoch, func(scope string) error {
		s.mu.Lock()
		s.resetPlaybackEpochLocked(b.Session.Epoch)
		b.PlaybackGeneration = s.playGeneration
		if s.searchEpoch == b.Session.Epoch {
			b.DiscoveryGeneration = s.searchGeneration
		}
		s.mu.Unlock()
		if _, e := s.store.Get(scope, "queue", "main", &b.Queue); e != nil {
			return e
		}
		if _, e := s.store.Get(scope, "bookmarks", "main", &b.Bookmarks); e != nil {
			return e
		}
		rows, e := s.store.List(scope, "progress")
		if e != nil {
			return e
		}
		for _, raw := range rows {
			var p model.Progress
			if json.Unmarshal(raw, &p) != nil {
				return model.Err("DISK", "本机收听记录损坏。")
			}
			r, e := s.loadProgress(scope, p.Item.ID)
			if e != nil {
				return e
			}
			b.History = append(b.History, r.Local)
		}
		syncRows, e := s.store.List(scope, "progress-sync")
		if e != nil {
			return e
		}
		seen := map[string]bool{}
		for _, p := range b.History {
			seen[p.Item.ID] = true
		}
		for _, raw := range syncRows {
			var r progressRecord
			if json.Unmarshal(raw, &r) != nil {
				return model.Err("DISK", "本机收听记录损坏。")
			}
			if r.Local.Item.ID != "" && !seen[r.Local.Item.ID] {
				b.History = append(b.History, r.Local)
			}
		}
		sort.Slice(b.History, func(i, j int) bool { return b.History[i].UpdatedAt > b.History[j].UpdatedAt })
		if len(b.History) > 100 {
			b.History = b.History[:100]
		}
		return nil
	})
	return b, e
}
func (s *Service) checkExperimental() error {
	if !s.Settings().ExperimentalAccount {
		return model.Err("EXPERIMENTAL_DISABLED", "账号接入默认关闭，请先阅读风险说明并手动启用。")
	}
	return nil
}
func (s *Service) SendCode(ctx context.Context, phone, area string) error {
	if e := s.checkExperimental(); e != nil {
		return e
	}
	if e := provider.ValidatePhone(phone, area); e != nil {
		return e
	}
	s.mu.Lock()
	if delay := time.Until(s.smsUntil); delay > 0 {
		s.mu.Unlock()
		return &model.AppError{Code: "RATE_LIMIT", Message: "为避免重复发送，请稍后再请求验证码。", RetryAfter: int(delay.Seconds()) + 1}
	}
	s.smsUntil = time.Now().Add(60 * time.Second)
	s.mu.Unlock()
	_, e := s.session.Guest(ctx, func(c context.Context) error { return s.p.SendCode(c, phone, area) })
	return e
}
func (s *Service) Login(ctx context.Context, phone, area, code string, remember bool) error {
	if e := s.checkExperimental(); e != nil {
		return e
	}
	return s.session.Login(ctx, phone, area, code, remember)
}
func (s *Service) Restore(ctx context.Context) error {
	if e := s.checkExperimental(); e != nil {
		return e
	}
	return s.session.Restore(ctx)
}
func (s *Service) clearTransientLocked() {
	s.searchEpoch = 0
	s.searchGeneration = 0
	s.subscriptionEpoch = 0
	s.subscriptionAttempts = nil
	s.subscriptionBusy = nil
	s.subscriptionPending = nil
	s.subscriptionConfirmed = nil
	s.commentReadEpoch = 0
	s.commentTargets = nil
	s.commentAttempts = nil
	s.commentEpoch = 0
	s.commentBusy = false
	for _, v := range s.stages {
		if v.cancel != nil {
			v.cancel()
		}
	}
	s.stages = map[string]*libraryStage{}
	if s.playCancel != nil {
		s.playCancel()
	}
	s.playID = ""
	s.playCancel = nil
	s.playEpoch = 0
	s.playGeneration = 0
	s.playOrdered = false
}
func (s *Service) Logout() error { return s.LogoutAt(s.Session().Epoch) }
func (s *Service) LogoutAt(epoch uint64) error {
	return s.session.LogoutAt(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.clearTransientLocked()
		if scope != "guest" {
			return s.store.DeleteScope(scope)
		}
		return nil
	})
}
func (s *Service) ClearCache(epoch uint64) error {
	return s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		for k, v := range s.stages {
			if v.cancel != nil {
				v.cancel()
			}
			delete(s.stages, k)
		}
		return s.store.DeleteKind(scope, "cache")
	})
}
func (s *Service) Reset(epoch uint64, confirm string) error {
	if confirm != "RESET" {
		return model.Err("CONFIRM_REQUIRED", "请确认清除全部本地数据。")
	}
	return s.session.LogoutAt(epoch, func(string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.clearTransientLocked()
		if e := s.store.Clear(); e != nil {
			return e
		}
		s.settings = model.DefaultSettings()
		s.startupError = nil
		s.log = nil
		return nil
	})
}
