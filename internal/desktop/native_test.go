package desktop

import "testing"

func TestQuitGateIsIdempotent(t *testing.T) {
	var gate QuitGate
	if !gate.Request() || gate.Request() {
		t.Fatal("duplicate close accepted")
	}
	if gate.Allowed() {
		t.Fatal("quit before flush")
	}
	gate.Allow()
	if !gate.Allowed() {
		t.Fatal("quit not allowed")
	}
}
