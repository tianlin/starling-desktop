package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"starling/internal/model"
	"starling/internal/provider"
	"starling/internal/security"
	"starling/internal/store"
	"starling/internal/testkit"
	"sync/atomic"
	"testing"
	"time"
)

// The live service probe uses a temporary database and an in-memory vault.
// Even an implementation regression cannot send an update or refresh tokens.
type readOnlyLiveProgressProvider struct {
	*provider.Client
	writes atomic.Int32
}

func (p *readOnlyLiveProgressProvider) WriteProgress(context.Context, string, []model.CloudProgress) error {
	p.writes.Add(1)
	return model.Err("LIVE_WRITE_BLOCKED", "Live service probe does not upload progress.")
}
func (p *readOnlyLiveProgressProvider) Refresh(context.Context, model.Credentials) (model.Credentials, error) {
	return model.Credentials{}, model.Err("UNAUTHORIZED", "Sign in through Starling before running this read-only probe.")
}

func TestLiveProgressPrepareReadOnly(t *testing.T) {
	if os.Getenv("STARLING_LIVE_PROGRESS") != "1" {
		t.Skip("set STARLING_LIVE_PROGRESS=1 for authorized read-only verification")
	}
	eid := os.Getenv("STARLING_LIVE_PROGRESS_EID")
	if !security.ValidID(eid) {
		t.Fatal("valid test episode required")
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
		t.Fatal("sign in first")
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	p := &readOnlyLiveProgressProvider{Client: provider.New()}
	s := New(p, db, &testkit.MemoryVault{Value: saved})
	defer db.Close()
	defer s.Close()
	settings := model.DefaultSettings()
	settings.ExperimentalAccount = true
	if err = s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err = s.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := p.ReadProgress(ctx, saved.Credentials.Access, []string{eid})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatal("test episode must have a cloud checkpoint")
	}
	args, _ := json.Marshal(map[string]any{"epoch": s.Session().Epoch, "eid": eid})
	raw := s.Dispatch(ctx, "progress.prepare", string(args))
	var result struct {
		OK   bool `json:"ok"`
		Data struct {
			Position float64 `json:"position"`
			Sync     struct {
				State string `json:"state"`
			} `json:"sync"`
		} `json:"data"`
		Error *model.AppError `json:"error"`
	}
	if err = json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("prepare failed: %v", result.Error)
	}
	if result.Data.Position != rows[0].Position {
		t.Fatal("prepared position differs from cloud; stop phone playback and retry")
	}
	if p.writes.Load() != 0 {
		t.Fatal("read-only prepare attempted a cloud update")
	}
	t.Logf("service prepared cloud positionSeconds=%.0f state=%s without uploads", result.Data.Position, result.Data.Sync.State)
}
