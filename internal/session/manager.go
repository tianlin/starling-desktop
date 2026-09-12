// Package session coordinates credentials, single-flight refresh and logout epochs.
package session

import (
	"context"
	"starling/internal/model"
	"starling/internal/provider"
	"starling/internal/security"
	"sync"
	"time"
)

type Snapshot struct {
	Epoch       uint64
	Scope       string
	State       string
	Identity    model.Identity
	context     context.Context
	credentials model.Credentials
}
type refreshFlight struct {
	epoch   uint64
	access  string
	done    chan struct{}
	err     error
	retryAt time.Time
}
type Manager struct {
	mu           sync.Mutex
	p            provider.Provider
	vault        security.Vault
	epoch        uint64
	state        string
	identity     model.Identity
	credentials  model.Credentials
	persistent   bool
	ctx          context.Context
	cancel       context.CancelFunc
	flight       *refreshFlight
	jobs         sync.WaitGroup
	closed       bool
	qrID         string
	qrUntil      time.Time
	qrBusy       bool
	smsBaseEpoch uint64
}

func New(p provider.Provider, v security.Vault) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{p: p, vault: v, epoch: 1, state: "guest", ctx: ctx, cancel: cancel}
}
func (m *Manager) View() model.SessionView {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := model.SessionView{Epoch: m.epoch, State: m.state, Persistent: m.persistent, StorageAvailable: m.vault.Available()}
	if m.identity.ID != "" {
		id := m.identity
		v.Identity = &id
	}
	return v
}
func (m *Manager) snapshotLocked() Snapshot {
	s := Snapshot{Epoch: m.epoch, Scope: "guest", State: m.state, Identity: m.identity, context: m.ctx, credentials: m.credentials}
	if m.identity.ID != "" {
		s.Scope = model.Scope(m.identity)
	}
	return s
}
func (m *Manager) Snapshot() Snapshot       { m.mu.Lock(); defer m.mu.Unlock(); return m.snapshotLocked() }
func (m *Manager) begin() (Snapshot, error) { return m.beginAt(m.View().Epoch) }
func (m *Manager) beginAt(expected uint64) (Snapshot, error) {
	return m.beginAttempt(expected, false)
}
func (m *Manager) beginAttempt(expected uint64, sms bool) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != expected {
		return Snapshot{}, model.ErrStale
	}
	if m.closed {
		return Snapshot{}, model.Err("CLOSED", "应用正在退出。")
	}
	if m.state == "connecting" {
		return Snapshot{}, model.Err("BUSY", "登录操作正在进行。")
	}
	if m.identity.ID != "" {
		return Snapshot{}, model.Err("ACCOUNT_ACTIVE", "请先退出当前账号，再连接其他账号。")
	}
	m.cancel()
	m.epoch++
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.state = "connecting"
	m.smsBaseEpoch = 0
	if sms {
		m.smsBaseEpoch = expected
	}
	m.qrID = ""
	m.qrUntil = time.Time{}
	m.qrBusy = false
	m.flight = nil
	return m.snapshotLocked(), nil
}
func (m *Manager) finishFailure(epoch uint64, e error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != epoch {
		return model.ErrStale
	}
	m.state = "guest"
	m.qrID = ""
	return e
}
func join(request, session context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(session)
	stop := context.AfterFunc(request, cancel)
	if request.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}
func (m *Manager) Login(ctx context.Context, phone, area, code string, remember bool) error {
	return m.LoginAt(ctx, m.View().Epoch, phone, area, code, remember)
}
func (m *Manager) LoginAt(ctx context.Context, epoch uint64, phone, area, code string, remember bool) error {
	if remember && !m.vault.Available() {
		return model.Err("SECURE_STORAGE", "系统保护存储不可用，请取消记住登录。")
	}
	s, e := m.beginAttempt(epoch, true)
	if e != nil {
		return e
	}
	combined, done := join(ctx, s.context)
	defer done()
	credentials, claimed, e := m.p.Login(combined, phone, area, code)
	if e != nil {
		return m.finishFailure(s.Epoch, e)
	}
	actual, e := m.p.Me(combined, credentials.Access)
	if e != nil {
		return m.finishFailure(s.Epoch, e)
	}
	if claimed.ID == "" || actual.ID == "" || actual.ID != claimed.ID {
		return m.finishFailure(s.Epoch, model.Err("IDENTITY_MISMATCH", "登录身份与个人资料不一致，未连接账号。"))
	}
	return m.establish(s.Epoch, combined, credentials, actual, remember)
}

// CancelLogin fences both a queued SMS request and its in-flight attempt.
// It never clears saved credentials or cancels a QR, restore, or newer login.
func (m *Manager) CancelLogin(base uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if base == 0 || m.closed {
		return model.ErrStale
	}
	matching := m.smsBaseEpoch == base && m.epoch == base+1
	if matching && m.identity.ID != "" {
		return model.Err("LOGIN_COMPLETED", "认证已完成，账号已连接。如需断开，请在账号管理中退出。")
	}
	queued := m.epoch == base && m.state == "guest" && m.identity.ID == ""
	if !queued && !(matching && (m.state == "connecting" || m.state == "guest")) {
		return model.ErrStale
	}
	m.cancel()
	m.epoch++
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.state = "guest"
	m.smsBaseEpoch = 0
	return nil
}
func (m *Manager) establish(epoch uint64, ctx context.Context, c model.Credentials, id model.Identity, persistent bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != epoch || m.closed {
		return model.ErrStale
	}
	if ctx.Err() != nil {
		m.state = "guest"
		return model.Err("CANCELLED", "登录已取消。")
	}
	if c.Access == "" || c.Refresh == "" || id.ID == "" {
		m.state = "guest"
		return model.Err("BAD_RESPONSE", "登录凭据或身份不完整。")
	}
	var e error
	if persistent {
		e = m.vault.Save(model.SavedSession{Credentials: c, Identity: id})
	} else {
		e = m.vault.Clear()
	}
	if e != nil {
		m.state = "guest"
		return e
	}
	m.credentials = c
	m.identity = id
	m.persistent = persistent
	m.state = "connected"
	m.qrID = ""
	return nil
}

// Restore always revalidates identity before exposing cached personal data.
func (m *Manager) Restore(ctx context.Context) error {
	expected := m.View().Epoch
	saved, e := m.vault.Load()
	if e != nil {
		return e
	}
	if saved.Credentials.Access == "" {
		return nil
	}
	s, e := m.beginAt(expected)
	if e != nil {
		return e
	}
	combined, done := join(ctx, s.context)
	defer done()
	c := saved.Credentials
	id, e := m.p.Me(combined, c.Access)
	if model.IsCode(e, "UNAUTHORIZED") {
		c, e = m.p.Refresh(combined, c)
		if e == nil {
			id, e = m.p.Me(combined, c.Access)
		}
	}
	if e != nil {
		return m.finishFailure(s.Epoch, e)
	}
	if id.ID == "" || id.ID != saved.Identity.ID {
		return m.finishFailure(s.Epoch, model.Err("IDENTITY_MISMATCH", "保存会话的身份发生变化，请退出后重新连接。"))
	}
	return m.establish(s.Epoch, combined, c, id, true)
}
func (m *Manager) Do(ctx context.Context, fn func(context.Context, string) error) (Snapshot, error) {
	return m.DoAt(ctx, m.View().Epoch, fn)
}

// DoAt binds the initial request as well as any refresh replay to the caller's epoch.
func (m *Manager) DoAt(ctx context.Context, epoch uint64, fn func(context.Context, string) error) (Snapshot, error) {
	s, snapshotErr := m.connectedSnapshot(epoch)
	if snapshotErr != nil {
		return s, snapshotErr
	}
	combined, done := join(ctx, s.context)
	defer done()
	e := fn(combined, s.credentials.Access)
	if model.IsCode(e, "UNAUTHORIZED") {
		if e = m.refresh(combined, s); e == nil {
			fresh, snapshotErr := m.connectedSnapshot(s.Epoch)
			if snapshotErr != nil {
				return s, snapshotErr
			}
			s = fresh
			e = fn(combined, s.credentials.Access)
			if model.IsCode(e, "UNAUTHORIZED") {
				m.mu.Lock()
				if m.epoch == s.Epoch {
					m.state = "needs_login"
				}
				m.mu.Unlock()
			}
		}
	}
	m.mu.Lock()
	stale := m.epoch != s.Epoch || m.closed
	m.mu.Unlock()
	if stale {
		return s, model.ErrStale
	}
	return s, e
}
func (m *Manager) Guest(ctx context.Context, fn func(context.Context) error) (Snapshot, error) {
	s := m.Snapshot()
	if s.Scope != "guest" || s.State != "guest" {
		return s, model.Err("UNAUTHORIZED", "请先处理当前账号状态。")
	}
	combined, done := join(ctx, s.context)
	defer done()
	e := fn(combined)
	m.mu.Lock()
	stale := m.epoch != s.Epoch
	m.mu.Unlock()
	if stale {
		return s, model.ErrStale
	}
	return s, e
}
func (m *Manager) refresh(ctx context.Context, s Snapshot) error {
	m.mu.Lock()
	if m.epoch != s.Epoch || m.closed {
		m.mu.Unlock()
		return model.ErrStale
	}
	if m.state != "connected" {
		m.mu.Unlock()
		return model.Err("UNAUTHORIZED", "账号需要重新登录。")
	}
	if m.credentials.Access != s.credentials.Access {
		m.mu.Unlock()
		return nil
	}
	f := m.flight
	if f == nil || f.epoch != s.Epoch || f.access != s.credentials.Access || (!f.retryAt.IsZero() && time.Now().After(f.retryAt)) {
		f = &refreshFlight{epoch: s.Epoch, access: s.credentials.Access, done: make(chan struct{})}
		m.flight = f
		m.jobs.Add(1)
		go m.performRefresh(s, f)
	}
	m.mu.Unlock()
	select {
	case <-ctx.Done():
		return model.Err("CANCELLED", "续期等待已取消。")
	case <-f.done:
		return f.err
	}
}
func (m *Manager) performRefresh(s Snapshot, f *refreshFlight) {
	defer m.jobs.Done()
	ctx, cancel := context.WithTimeout(s.context, 25*time.Second)
	defer cancel()
	next, e := m.p.Refresh(ctx, s.credentials)
	m.mu.Lock()
	defer m.mu.Unlock()
	defer close(f.done)
	if m.epoch != s.Epoch || m.closed {
		f.err = model.ErrStale
		return
	}
	if e == nil && (next.Access == "" || next.Refresh == "") {
		e = model.Err("BAD_RESPONSE", "平台续期响应不完整。")
	}
	if e == nil && m.persistent {
		e = m.vault.Save(model.SavedSession{Credentials: next, Identity: m.identity})
		if e != nil {
			m.state = "needs_login"
		}
	}
	if e == nil {
		m.credentials = next
	} else {
		if model.IsCode(e, "UNAUTHORIZED") {
			m.state = "needs_login"
		}
		f.err = e
		delay := time.Second
		if x := model.PublicError(e); x.RetryAfter > 0 {
			delay = time.Duration(x.RetryAfter) * time.Second
		}
		f.retryAt = time.Now().Add(delay)
	}
}

// Commit serializes the epoch check and persistence with logout. Never check then write outside this lock.
func (m *Manager) Commit(epoch uint64, fn func(scope string) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != epoch || m.closed {
		return model.ErrStale
	}
	if m.state == "connecting" {
		return model.Err("BUSY", "账号正在连接。")
	}
	scope := "guest"
	if m.identity.ID != "" {
		scope = model.Scope(m.identity)
	}
	return fn(scope)
}
func (m *Manager) Logout(cleanup func(string) error) error {
	return m.LogoutAt(m.View().Epoch, cleanup)
}

// LogoutAt verifies the epoch under the same lock as cleanup; stale UI cannot log out a newer account.
// cleanup also runs for the guest scope, allowing an atomic application-wide reset.
func (m *Manager) LogoutAt(epoch uint64, cleanup func(string) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != epoch || m.closed {
		return model.ErrStale
	}
	old := m.snapshotLocked().Scope
	m.cancel()
	m.epoch++
	m.credentials = model.Credentials{}
	m.identity = model.Identity{}
	m.persistent = false
	m.state = "guest"
	m.flight = nil
	m.ctx, m.cancel = context.WithCancel(context.Background())
	e := m.vault.Clear()
	if cleanup != nil {
		if er := cleanup(old); e == nil {
			e = er
		}
	}
	return e
}
func (m *Manager) Close() {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		m.cancel()
	}
	m.mu.Unlock()
	m.jobs.Wait()
}

// A replay must keep the identity epoch of its initial request, even if logout races refresh completion.
func (m *Manager) connectedSnapshot(epoch uint64) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != epoch || m.closed {
		return Snapshot{}, model.ErrStale
	}
	if m.state != "connected" {
		return Snapshot{}, model.Err("UNAUTHORIZED", "账号需要重新登录。")
	}
	return m.snapshotLocked(), nil
}
