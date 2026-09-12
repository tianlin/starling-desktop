package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"starling/internal/model"
	"starling/internal/store"
	"starling/internal/testkit"
	"strings"
	"testing"
)

func fixture(t *testing.T, f *testkit.Fake) *Service {
	t.Helper()
	db, e := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if e != nil {
		t.Fatal(e)
	}
	s := New(f, db, &testkit.MemoryVault{})
	t.Cleanup(func() { s.Close(); db.Close() })
	return s
}
func connect(t *testing.T, s *Service) uint64 {
	t.Helper()
	settings := model.DefaultSettings()
	settings.ExperimentalAccount = true
	if e := s.SaveSettings(settings); e != nil {
		t.Fatal(e)
	}
	if e := s.Login(context.Background(), "00000000000", "+86", "123456", true); e != nil {
		t.Fatal(e)
	}
	return s.session.View().Epoch
}
func item(id string) model.Item {
	return model.Item{ID: id, Kind: "episode", Title: "测试单集", SourceURL: "https://www.xiaoyuzhoufm.com/episode/" + id}
}

const idA = "64db2d493fa4090b744c313c"
const idB = "64db2d493fa4090b744c313d"

func TestAccountOffByDefault(t *testing.T) {
	s := fixture(t, &testkit.Fake{})
	if e := s.Login(context.Background(), "00000000000", "+86", "123456", false); !model.IsCode(e, "EXPERIMENTAL_DISABLED") {
		t.Fatal(e)
	}
}
func TestQueueDedupAndNext(t *testing.T) {
	s := fixture(t, &testkit.Fake{})
	epoch := s.session.View().Epoch
	s.Queue(epoch, "append", item(idA))
	s.Queue(epoch, "append", item(idB))
	s.Queue(epoch, "append", item(idA))
	q, e := s.Queue(epoch, "read", model.Item{})
	if e != nil || len(q) != 2 || q[0].ID != idA {
		t.Fatal(q, e)
	}
	q, e = s.Queue(epoch, "next", item(idB))
	if e != nil || q[0].ID != idB || len(q) != 2 {
		t.Fatal(q, e)
	}
	q, e = s.Queue(epoch, "remove", item(idB))
	if e != nil || len(q) != 1 || q[0].ID != idA {
		t.Fatal(q, e)
	}
}
func TestLogoutIsolation(t *testing.T) {
	s := fixture(t, &testkit.Fake{})
	guest := s.session.View().Epoch
	s.Queue(guest, "append", item(idA))
	epoch := connect(t, s)
	s.Queue(epoch, "append", item(idB))
	if e := s.Logout(); e != nil {
		t.Fatal(e)
	}
	q, e := s.Queue(s.session.View().Epoch, "read", model.Item{})
	if e != nil || len(q) != 1 || q[0].ID != idA {
		t.Fatal(q, e)
	}
	if _, e = s.Queue(epoch, "append", item(idB)); !model.IsCode(e, "STALE_SESSION") {
		t.Fatal(e)
	}
	var saved []model.Item
	if ok, _ := s.store.Get("xiaoyuzhou:user-a", "queue", "main", &saved); ok {
		t.Fatal("private queue remained")
	}
}
func TestProgressRejectsInvalidAndResumes(t *testing.T) {
	s := fixture(t, &testkit.Fake{})
	epoch := s.session.View().Epoch
	if e := s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: -1, Duration: 120}); e == nil {
		t.Fatal("negative progress")
	}
	if e := s.SaveProgress(epoch, model.Progress{Item: item(idA), Position: 42, Duration: 120}); e != nil {
		t.Fatal(e)
	}
	p, e := s.Resolve(context.Background(), epoch, idA, "request1")
	if e != nil || p.Position != 42 {
		t.Fatal(p, e)
	}
}
func TestNoCredentialInDispatch(t *testing.T) {
	s := fixture(t, &testkit.Fake{})
	connect(t, s)
	raw := s.Dispatch(context.Background(), "bootstrap", `{}`)
	if strings.Contains(raw, "refresh-old") || strings.Contains(raw, `"access"`) {
		t.Fatal(raw)
	}
	var v map[string]any
	if json.Unmarshal([]byte(raw), &v) != nil || v["ok"] != true {
		t.Fatal(raw)
	}
}
func TestUnknownBridgeMethodRejected(t *testing.T) {
	s := fixture(t, &testkit.Fake{})
	if raw := s.Dispatch(context.Background(), "httpProxy", `{"url":"http://127.0.0.1"}`); !strings.Contains(raw, "UNSUPPORTED") {
		t.Fatal(raw)
	}
}
func TestUnknownSettingsRejected(t *testing.T) {
	s := fixture(t, &testkit.Fake{})
	raw := s.Dispatch(context.Background(), "settings.save", `{"volume":0.5,"rate":1,"closeBehavior":"ask","experimentalAccount":false,"execute":"evil"}`)
	if !strings.Contains(raw, "INVALID_REQUEST") {
		t.Fatal(raw)
	}
}
