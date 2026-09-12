// Package desktop isolates native integration from both the player and platform provider.
package desktop

import "sync"

type Hooks struct{ Show, Toggle, Quit, Pause func() }
type Info struct {
	Platform string `json:"platform"`
	Tray     bool   `json:"tray"`
	MediaKey bool   `json:"mediaKey"`
	Demo     bool   `json:"demo"`
	Message  string `json:"message,omitempty"`
}

// QuitGate allows only one close handshake and releases exit after progress has been flushed.
type QuitGate struct {
	mu                 sync.Mutex
	requested, allowed bool
}

func (g *QuitGate) Request() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.requested {
		return false
	}
	g.requested = true
	return true
}
func (g *QuitGate) Allow()          { g.mu.Lock(); g.allowed = true; g.mu.Unlock() }
func (g *QuitGate) Allowed() bool   { g.mu.Lock(); defer g.mu.Unlock(); return g.allowed }
func (g *QuitGate) Requested() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.requested }
