package provider

import (
	"context"
	"os"
	"path/filepath"
	"starling/internal/security"
	"testing"
	"time"
)

// Opt-in read only: no credential refresh, cloud update or content logging.
func TestLiveProgressReadOnly(t *testing.T) {
	if os.Getenv("STARLING_LIVE_PROGRESS") != "1" {
		t.Skip("set STARLING_LIVE_PROGRESS=1 for an authorized account read")
	}
	eid := os.Getenv("STARLING_LIVE_PROGRESS_EID")
	if !security.ValidID(eid) {
		t.Fatal("set STARLING_LIVE_PROGRESS_EID to a valid test episode ID")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := security.NewVault(filepath.Join(base, "Starling", "session.vault")).Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Credentials.Access == "" {
		t.Fatal("no saved Starling session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rows, err := New().ReadProgress(ctx, saved.Credentials.Access, []string{eid})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("progress records=%d", len(rows))
	for _, row := range rows {
		_, err := time.Parse(time.RFC3339Nano, row.PlayedAt)
		t.Logf("positionSeconds=%.0f validPlaybackTime=%v", row.Position, err == nil)
	}
}
