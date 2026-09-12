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

func TestStartupSettingsWarningClearsAfterRepair(t *testing.T) {
	for _, operation := range []string{"save", "reset"} {
		t.Run(operation, func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			if err := db.Put("global", "settings", "main", model.Settings{Volume: -1}); err != nil {
				t.Fatal(err)
			}
			s := New(&testkit.Fake{}, db, &testkit.MemoryVault{})
			t.Cleanup(s.Close)
			checkWarning := func(want bool) {
				t.Helper()
				b, err := s.Bootstrap()
				if err != nil || (b.Warning != nil) != want {
					t.Fatalf("warning=%v, err=%v, want warning=%v", b.Warning, err, want)
				}
			}
			checkWarning(true)
			if operation == "save" {
				if err := s.SaveSettings(model.Settings{Volume: -1}); err == nil {
					t.Fatal("invalid repair succeeded")
				}
				checkWarning(true)
				err = s.SaveSettings(model.DefaultSettings())
			} else {
				if err := s.Reset(s.Session().Epoch, ""); err == nil {
					t.Fatal("unconfirmed reset succeeded")
				}
				checkWarning(true)
				err = s.Reset(s.Session().Epoch, "RESET")
			}
			if err != nil {
				t.Fatal(err)
			}
			checkWarning(false)
		})
	}
}

func TestLoginDiagnosticsPreserveAllowedActionNames(t *testing.T) {
	for _, action := range []string{"account.qrStart", "account.qrPoll", "account.qrCancel", "account.cancelLogin", strings.Repeat("private-input-", 5)} {
		t.Run(action, func(t *testing.T) {
			s := fixture(t, &testkit.Fake{})
			// qrStart fails with experimental access disabled; other known
			// actions reject the unknown payload field before contacting a provider.
			raw := s.Dispatch(context.Background(), action, `{"private":"payload-secret"}`)
			var result struct {
				OK bool `json:"ok"`
			}
			if err := json.Unmarshal([]byte(raw), &result); err != nil || result.OK {
				t.Fatalf("expected action failure: %s, err=%v", raw, err)
			}
			var report struct {
				Data struct {
					Events []diagnosticEvent `json:"events"`
				} `json:"data"`
			}
			raw = s.Dispatch(context.Background(), "diagnostics", `{}`)
			if err := json.Unmarshal([]byte(raw), &report); err != nil {
				t.Fatal(err)
			}
			want := action
			if strings.HasPrefix(action, "private-input-") {
				want = "unknown"
			}
			if len(report.Data.Events) != 1 || report.Data.Events[0].Action != want || report.Data.Events[0].Code == "" {
				t.Fatalf("diagnostics=%s, want action=%s", raw, want)
			}
			if strings.Contains(raw, "payload-secret") || strings.Contains(raw, "private-input-") {
				t.Fatalf("diagnostics leaked input: %s", raw)
			}
		})
	}
}
