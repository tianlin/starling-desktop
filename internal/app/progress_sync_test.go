package app

import (
	"context"
	"errors"
	"path/filepath"
	"starling/internal/model"
	"starling/internal/store"
	"starling/internal/testkit"
	"testing"
)

type progressFake struct {
	testkit.Fake
	cloud    []model.CloudProgress
	readErr  error
	writes   int
	onRead   func()
	onWrite  func()
	writeErr error
}

func (f *progressFake) ReadProgress(context.Context, string, []string) ([]model.CloudProgress, error) {
	if f.onRead != nil {
		fn := f.onRead
		f.onRead = nil
		fn()
	}
	return f.cloud, f.readErr
}
func (f *progressFake) WriteProgress(_ context.Context, _ string, p []model.CloudProgress) error {
	f.writes++
	f.cloud = p
	if f.onWrite != nil {
		fn := f.onWrite
		f.onWrite = nil
		fn()
	}
	return f.writeErr
}

func TestProgressLateReadCannotUploadNewRevision(t *testing.T) {
	f := &progressFake{}
	s := syncFixture(t, f)
	epoch := connect(t, s)
	s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 80})
	f.onRead = func() { s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 20}) }
	st, e := s.ProgressRetry(context.Background(), epoch)
	if e != nil || f.writes != 0 || st.Pending != 1 {
		t.Fatal(st, e, f.writes)
	}
}
func TestProgressLateACKKeepsNewRevision(t *testing.T) {
	f := &progressFake{}
	s := syncFixture(t, f)
	epoch := connect(t, s)
	s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 80})
	f.onWrite = func() { s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 20}) }
	st, e := s.ProgressRetry(context.Background(), epoch)
	if e != nil || st.Pending != 1 {
		t.Fatal(st, e)
	}
	st, e = s.ProgressRetry(context.Background(), epoch)
	if e != nil || st.Pending != 0 || f.cloud[0].Position != 20 {
		t.Fatal(st, e, f.cloud)
	}
}
func TestProgressUncertainWriteReadbackNoReplay(t *testing.T) {
	f := &progressFake{writeErr: errors.New("lost reply")}
	s := syncFixture(t, f)
	epoch := connect(t, s)
	s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 20.8})
	s.ProgressRetry(context.Background(), epoch)
	f.cloud[0].Position = 20
	f.writeErr = nil
	st, e := s.ProgressRetry(context.Background(), epoch)
	if e != nil || st.Pending != 0 || f.writes != 1 {
		t.Fatal(st, e, f.writes)
	}
}
func TestProgressDisabledSaveClearsOldPending(t *testing.T) {
	f := &progressFake{}
	s := syncFixture(t, f)
	epoch := connect(t, s)
	s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 80})
	settings := s.Settings()
	settings.ProgressSyncDisabled = true
	s.SaveSettings(settings)
	s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 20})
	settings.ProgressSyncDisabled = false
	s.SaveSettings(settings)
	st, e := s.ProgressRetry(context.Background(), epoch)
	if e != nil || st.Pending != 0 || f.writes != 0 {
		t.Fatal(st, e, f.writes)
	}
}
func TestProgressOldEpochCannotPersist(t *testing.T) {
	f := &progressFake{}
	s := syncFixture(t, f)
	epoch := connect(t, s)
	s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 80})
	f.onRead = func() { s.LogoutAt(epoch) }
	_, e := s.ProgressRetry(context.Background(), epoch)
	if !model.IsCode(e, "STALE_SESSION") || f.writes != 0 {
		t.Fatal(e, f.writes)
	}
}

func TestProgressDisableDuringReadCancelsWrite(t *testing.T) {
	f := &progressFake{}
	s := syncFixture(t, f)
	epoch := connect(t, s)
	s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 80})
	f.onRead = func() {
		v := s.Settings()
		v.ProgressSyncDisabled = true
		if e := s.SaveSettings(v); e != nil {
			t.Fatal(e)
		}
	}
	st, e := s.ProgressRetry(context.Background(), epoch)
	if e != nil || st.State != "disabled" || f.writes != 0 {
		t.Fatal(st, e, f.writes)
	}
}
func TestProgressOutboxSurvivesServiceRestart(t *testing.T) {
	f := &progressFake{}
	s := syncFixture(t, f)
	epoch := connect(t, s)
	s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 31})
	s.Close()
	next := New(f, s.store, &testkit.MemoryVault{})
	defer next.Close()
	epoch = connect(t, next)
	st, e := next.ProgressRetry(context.Background(), epoch)
	if e != nil || st.Pending != 0 || f.writes != 1 || f.cloud[0].Position != 31 {
		t.Fatal(st, e, f.cloud)
	}
}
func TestProgressLegacyHistoryNeverBulkUploads(t *testing.T) {
	f := &progressFake{}
	s := syncFixture(t, f)
	epoch := connect(t, s)
	e := s.session.Commit(epoch, func(scope string) error {
		return s.store.Put(scope, "progress", idA, model.Progress{Item: item(idA), Position: 20, UpdatedAt: model.Now()})
	})
	if e != nil {
		t.Fatal(e)
	}
	s.ProgressRetry(context.Background(), epoch)
	if f.writes != 0 {
		t.Fatal(f.writes)
	}
}

func TestProgressIdleRetryDoesNotInventSuccess(t *testing.T) {
	f := &progressFake{}
	s := syncFixture(t, f)
	epoch := connect(t, s)
	st, e := s.ProgressRetry(context.Background(), epoch)
	if e != nil || st.LastSuccess != "" || st.State != "idle" {
		t.Fatal(st, e)
	}
	if e = s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 20}); e != nil {
		t.Fatal(e)
	}
	st, e = s.ProgressRetry(context.Background(), epoch)
	if e != nil || st.LastSuccess == "" {
		t.Fatal(st, e)
	}
	first := st.LastSuccess
	st, e = s.ProgressRetry(context.Background(), epoch)
	if e != nil || st.LastSuccess != first {
		t.Fatal(st, first, e)
	}
}

func TestProgressConflictRetryPreservesSuccessTime(t *testing.T) {
	f := &progressFake{}
	s := syncFixture(t, f)
	epoch := connect(t, s)
	if e := s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 80}); e != nil {
		t.Fatal(e)
	}
	f.cloud = []model.CloudProgress{{EpisodeID: idA, Position: 20, PlayedAt: model.Now()}}
	p, e := s.PrepareProgress(context.Background(), epoch, idA)
	if e != nil || p.Conflict == nil {
		t.Fatal(p, e)
	}
	st, e := s.ProgressRetry(context.Background(), epoch)
	if e != nil || st.State != "conflict" || st.LastSuccess != p.Sync.LastSuccess || f.writes != 0 {
		t.Fatal(st, p.Sync, e)
	}
}
func syncFixture(t *testing.T, f *progressFake) *Service {
	t.Helper()
	db, e := store.Open(filepath.Join(t.TempDir(), "sync.db"))
	if e != nil {
		t.Fatal(e)
	}
	s := New(f, db, &testkit.MemoryVault{})
	t.Cleanup(func() { s.Close(); db.Close() })
	return s
}
func TestProgressOfflineOutboxAndRewind(t *testing.T) {
	f := &progressFake{readErr: errors.New("offline")}
	s := syncFixture(t, f)
	epoch := connect(t, s)
	if e := s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 80, Duration: 120}); e != nil {
		t.Fatal(e)
	}
	st, _ := s.ProgressRetry(context.Background(), epoch)
	if st.Pending != 1 || f.writes != 0 {
		t.Fatal(st, f.writes)
	}
	if e := s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 20, Duration: 120}); e != nil {
		t.Fatal(e)
	}
	f.readErr = nil
	st, _ = s.ProgressRetry(context.Background(), epoch)
	if st.Pending != 0 || f.writes != 1 || f.cloud[0].Position != 20 {
		t.Fatal(st, f.cloud)
	}
}
func TestProgressAmbiguousConflictAndChoice(t *testing.T) {
	f := &progressFake{}
	s := syncFixture(t, f)
	epoch := connect(t, s)
	if e := s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 80, Duration: 120}); e != nil {
		t.Fatal(e)
	}
	f.cloud = []model.CloudProgress{{EpisodeID: idA, Position: 20, PlayedAt: model.Now()}}
	p, e := s.PrepareProgress(context.Background(), epoch, idA)
	if e != nil || p.Conflict == nil {
		t.Fatal(p, e)
	}
	chosen, e := s.ChooseProgress(context.Background(), epoch, idA, p.Conflict.Token, "cloud")
	if e != nil || chosen.Position != 20 {
		t.Fatal(chosen, e)
	}
	if _, e = s.ChooseProgress(context.Background(), epoch, idA, p.Conflict.Token, "local"); e == nil {
		t.Fatal("stale choice accepted")
	}
}
