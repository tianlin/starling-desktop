package session

import (
	"context"
	"starling/internal/model"
)

// DoOnceAt deliberately never refreshes credentials or replays fn. A transport
// cancellation cannot establish that a remote mutation was rolled back.
func (m *Manager) DoOnceAt(ctx context.Context, epoch uint64, fn func(context.Context, string) error) (Snapshot, error) {
	s, err := m.connectedSnapshot(epoch)
	if err != nil {
		return s, err
	}
	combined, done := join(ctx, s.context)
	defer done()
	if combined.Err() != nil {
		return s, model.Err("CANCELLED", "请求尚未发送，操作已取消。")
	}
	err = fn(combined, s.credentials.Access)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != epoch || m.closed {
		return s, model.ErrStale
	}
	// A concurrent successful read refresh must not be invalidated by the old token.
	if model.IsCode(err, "UNAUTHORIZED") && m.credentials.Access == s.credentials.Access {
		m.state = "needs_login"
	}
	return s, err
}
