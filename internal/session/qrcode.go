package session

import (
	"context"
	"starling/internal/model"
	"starling/internal/provider"
	"time"
)

type qrProvider interface {
	CreateQR(context.Context) (provider.QRCode, error)
	PollQR(context.Context, string) (string, model.Credentials, error)
	QRIdentity(context.Context, string) (model.Identity, error)
}

func (m *Manager) StartQR(ctx context.Context) (provider.QRCode, error) {
	p, ok := m.p.(qrProvider)
	if !ok {
		return provider.QRCode{}, model.Err("UNSUPPORTED", "此运行环境不支持真实扫码。")
	}
	s, e := m.begin()
	if e != nil {
		return provider.QRCode{}, e
	}
	combined, done := join(ctx, s.context)
	defer done()
	q, e := p.CreateQR(combined)
	if e != nil {
		return provider.QRCode{}, m.finishFailure(s.Epoch, e)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != s.Epoch || m.closed || combined.Err() != nil {
		return provider.QRCode{}, model.ErrStale
	}
	m.qrID = q.ID
	m.qrUntil = time.Now().Add(3 * time.Minute)
	m.qrBusy = false
	return q, nil
}

func (m *Manager) CancelQR(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == "" || m.state != "connecting" || id != m.qrID {
		return
	}
	m.cancel()
	m.epoch++
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.state = "guest"
	m.qrID = ""
	m.qrBusy = false
}

func (m *Manager) PollQR(ctx context.Context, id string, remember bool) (string, error) {
	p, ok := m.p.(qrProvider)
	if !ok {
		return "", model.Err("UNSUPPORTED", "此运行环境不支持真实扫码。")
	}
	m.mu.Lock()
	if id == "" || id != m.qrID || m.state != "connecting" || m.closed {
		m.mu.Unlock()
		return "", model.ErrStale
	}
	s := m.snapshotLocked()
	if time.Now().After(m.qrUntil) {
		m.mu.Unlock()
		return "", m.finishFailure(s.Epoch, model.Err("QR_EXPIRED", "二维码已过期，请重新生成。"))
	}
	if m.qrBusy {
		m.mu.Unlock()
		return "", model.Err("BUSY", "正在检查扫码状态。")
	}
	m.qrBusy = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.epoch == s.Epoch {
			m.qrBusy = false
		}
		m.mu.Unlock()
	}()
	combined, done := join(ctx, s.context)
	defer done()
	status, creds, e := p.PollQR(combined, id)
	if e != nil {
		return "", m.finishFailure(s.Epoch, e)
	}
	if status != "CONFIRMED" {
		return status, nil
	}
	claimed, e := p.QRIdentity(combined, creds.Access)
	if e != nil {
		return "", m.finishFailure(s.Epoch, e)
	}
	actual, e := m.p.Me(combined, creds.Access)
	if e != nil {
		return "", m.finishFailure(s.Epoch, e)
	}
	if actual.ID == "" || actual.ID != claimed.ID {
		return "", m.finishFailure(s.Epoch, model.Err("IDENTITY_MISMATCH", "扫码身份与听众身份不一致，未连接账号。"))
	}
	if remember && !m.vault.Available() {
		return "", m.finishFailure(s.Epoch, model.Err("SECURE_STORAGE", "系统保护存储不可用，请取消记住登录。"))
	}
	if e = m.establish(s.Epoch, combined, creds, actual, remember); e != nil {
		return "", e
	}
	return "CONFIRMED", nil
}
