package app

import (
	"context"
	"encoding/json"
	"starling/internal/model"
	"starling/internal/testkit"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type playbackReply struct {
	OK    bool            `json:"ok"`
	Error *model.AppError `json:"error"`
}

func orderedCall(s *Service, ctx context.Context, action string, epoch, generation uint64, id, requestID string) playbackReply {
	payload := map[string]any{"epoch": epoch, "generation": generation, "requestId": requestID}
	if action == "playback.resolve" {
		payload["id"] = id
	}
	raw, _ := json.Marshal(payload)
	var result playbackReply
	if err := json.Unmarshal([]byte(s.Dispatch(ctx, action, string(raw))), &result); err != nil {
		panic(err)
	}
	return result
}

func requirePlaybackCode(t *testing.T, result playbackReply, code string) {
	t.Helper()
	if code == "" {
		if !result.OK || result.Error != nil {
			t.Fatalf("expected success: %+v", result)
		}
	} else if result.OK || result.Error == nil || result.Error.Code != code {
		t.Fatalf("expected %s: %+v", code, result)
	}
}

func waitPlaybackStarted(t *testing.T, started <-chan context.Context, result <-chan playbackReply) context.Context {
	t.Helper()
	select {
	case ctx := <-started:
		return ctx
	case reply := <-result:
		t.Fatalf("resolve returned before reaching provider: %+v", reply)
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	return nil
}

func playableItem(id string) model.Item {
	it := item(id)
	it.MediaURL = "https://media.xyzcdn.net/fixture.wav"
	return it
}

func requirePlaybackCancelled(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("provider context was not cancelled")
	}
}

func TestPlaybackQueuedCancellationAndLateResolvePreserveNewSelection(t *testing.T) {
	for _, cancelOrder := range []string{"before-new", "after-new", "after-late-resolve"} {
		t.Run(cancelOrder, func(t *testing.T) {
			started := make(chan context.Context, 1)
			release := make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			var oldCalls atomic.Int32
			s := fixture(t, &testkit.Fake{DetailFunc: func(ctx context.Context, _, _, id string) (model.Item, error) {
				if id == idA {
					oldCalls.Add(1)
					return playableItem(id), nil
				}
				started <- ctx
				select {
				case <-release:
					return playableItem(id), nil
				case <-ctx.Done():
					return model.Item{}, ctx.Err()
				}
			}})
			epoch := s.Session().Epoch
			cancelOld := func() {
				requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.cancel", epoch, 1, "", "old"), "")
			}
			if cancelOrder == "before-new" {
				cancelOld()
			}
			result := make(chan playbackReply, 1)
			go func() { result <- orderedCall(s, context.Background(), "playback.resolve", epoch, 2, idB, "new") }()
			newCtx := waitPlaybackStarted(t, started, result)
			if cancelOrder == "after-new" {
				cancelOld()
			}
			requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.resolve", epoch, 1, idA, "old"), "CANCELLED")
			if cancelOrder == "after-late-resolve" {
				cancelOld()
			}
			if oldCalls.Load() != 0 || newCtx.Err() != nil {
				t.Fatal("late cancelled selection reached the provider or cancelled the new selection")
			}
			releaseOnce.Do(func() { close(release) })
			requirePlaybackCode(t, <-result, "")
		})
	}
}

func TestPlaybackNewSelectionFencesOldProviderResult(t *testing.T) {
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	s := fixture(t, &testkit.Fake{DetailFunc: func(ctx context.Context, _, _, id string) (model.Item, error) {
		if id == idA {
			started <- ctx
			<-release // Deliberately ignore cancellation and return a late success.
		}
		return playableItem(id), nil
	}})
	epoch := s.Session().Epoch
	result := make(chan playbackReply, 1)
	go func() { result <- orderedCall(s, context.Background(), "playback.resolve", epoch, 1, idA, "old") }()
	oldCtx := waitPlaybackStarted(t, started, result)
	requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.resolve", epoch, 2, idB, "new"), "")
	requirePlaybackCancelled(t, oldCtx)
	requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.cancel", epoch, 1, "", "old"), "")
	once.Do(func() { close(release) })
	requirePlaybackCode(t, <-result, "CANCELLED")
}

func TestPlaybackCancelledQueuedAndDuplicateGenerationsNeverResolve(t *testing.T) {
	var calls atomic.Int32
	s := fixture(t, &testkit.Fake{DetailFunc: func(_ context.Context, _, _, id string) (model.Item, error) {
		calls.Add(1)
		return playableItem(id), nil
	}})
	epoch := s.Session().Epoch
	requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.cancel", epoch, 3, "", "queued"), "")
	requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.resolve", epoch, 3, idA, "queued"), "CANCELLED")
	requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.resolve", epoch, 4, idA, "current"), "")
	requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.resolve", epoch, 4, idB, "duplicate"), "CANCELLED")
	if calls.Load() != 1 {
		t.Fatal("queued or duplicate generation reached provider")
	}
	var boot struct {
		Data struct {
			PlaybackGeneration uint64 `json:"playbackGeneration"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(s.Dispatch(context.Background(), "bootstrap", `{}`)), &boot); err != nil || boot.Data.PlaybackGeneration != 4 {
		t.Fatalf("bootstrap lost generation: %+v err=%v", boot, err)
	}
}

func TestPlaybackOrderedAdmissionRejectsInvalidAndLegacyRequests(t *testing.T) {
	started := make(chan context.Context, 1)
	s := fixture(t, &testkit.Fake{DetailFunc: func(ctx context.Context, _, _, id string) (model.Item, error) {
		started <- ctx
		<-ctx.Done()
		return model.Item{}, ctx.Err()
	}})
	epoch := s.Session().Epoch
	result := make(chan playbackReply, 1)
	go func() { result <- orderedCall(s, context.Background(), "playback.resolve", epoch, 5, idB, "current") }()
	currentCtx := waitPlaybackStarted(t, started, result)
	for _, generation := range []uint64{0, 1 << 53} {
		for _, action := range []string{"playback.resolve", "playback.cancel"} {
			requirePlaybackCode(t, orderedCall(s, context.Background(), action, epoch, generation, idA, "invalid"), "INVALID_REQUEST")
		}
	}
	if _, err := s.Resolve(context.Background(), epoch, idA, "legacy"); !model.IsCode(err, "INVALID_REQUEST") {
		t.Fatalf("legacy request bypassed ordering: %v", err)
	}
	if err := s.CancelResolve(epoch, "current"); !model.IsCode(err, "INVALID_REQUEST") {
		t.Fatalf("legacy cancel bypassed ordering: %v", err)
	}
	requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.cancel", epoch, 5, "", "different-id"), "")
	if currentCtx.Err() != nil {
		t.Fatal("invalid or mismatched operation cancelled the current selection")
	}
	requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.cancel", epoch, 5, "", "current"), "")
	requirePlaybackCancelled(t, currentCtx)
	if (<-result).OK {
		t.Fatal("cancelled provider returned success")
	}
}

func TestPlaybackGenerationIsScopedToSessionEpoch(t *testing.T) {
	s := fixture(t, &testkit.Fake{})
	epoch := s.Session().Epoch
	if _, err := s.Resolve(context.Background(), epoch, idA, "legacy"); err != nil {
		t.Fatal(err)
	}
	requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.resolve", epoch, 50, idA, "old"), "")
	newEpoch := connect(t, s) // Login changes epoch without clearing service transients.
	boot, err := s.Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(boot)
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	if fields["playbackGeneration"] != float64(0) {
		t.Fatalf("new epoch retained old playback generation: %s", raw)
	}
	requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.cancel", epoch, 100, "", "old"), "STALE_SESSION")
	requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.resolve", newEpoch, 1, idB, "new"), "")
	if err := s.LogoutAt(newEpoch); err != nil {
		t.Fatal(err)
	}
	requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.resolve", s.Session().Epoch, 1, idA, "guest"), "")
}

func TestPlaybackNewerQueuedCancellationStopsOlderActiveSelection(t *testing.T) {
	started := make(chan context.Context, 1)
	var calls atomic.Int32
	s := fixture(t, &testkit.Fake{DetailFunc: func(ctx context.Context, _, _, id string) (model.Item, error) {
		calls.Add(1)
		started <- ctx
		<-ctx.Done()
		return model.Item{}, ctx.Err()
	}})
	epoch := s.Session().Epoch
	result := make(chan playbackReply, 1)
	go func() {
		result <- orderedCall(s, context.Background(), "playback.resolve", epoch, 1, idA, "older-active")
	}()
	oldCtx := waitPlaybackStarted(t, started, result)
	requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.cancel", epoch, 2, "", "new-queued"), "")
	requirePlaybackCancelled(t, oldCtx)
	requirePlaybackCode(t, <-result, "CANCELLED")
	requirePlaybackCode(t, orderedCall(s, context.Background(), "playback.resolve", epoch, 2, idB, "new-queued"), "CANCELLED")
	if calls.Load() != 1 {
		t.Fatal("cancelled queued selection reached provider")
	}
}
