package provider

import (
	"encoding/json"
	"testing"
)

func TestDecodeMediaSelection(t *testing.T) {
	const primary = "https://media.xyzcdn.net/primary.m4a"
	const backup = "https://media.xyzcdn.net/backup.mp4a"
	const external = "https://jt.ximalaya.com/episode.m4a"
	for _, tc := range []struct {
		name, enclosure, source, backup, mode, want string
	}{
		{"preserve supported enclosure", primary, "", backup, "PUBLIC", primary},
		{"use supported source", external, primary, backup, "PUBLIC", primary},
		{"use official public backup", external, external, backup, "PUBLIC", backup},
		{"missing primary uses public backup", "", "", backup, "PUBLIC", backup},
		{"untrusted backup stays rejected", external, "", "https://evil.test/audio.m4a", "PUBLIC", external},
		{"private backup is not a fallback", external, "", backup, "PRIVATE", external},
		{"unspecified backup access is not a fallback", external, "", backup, "", external},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"eid": "64db2d493fa4090b744c313c", "title": "test", "payType": "FREE",
				"enclosure": map[string]any{"url": tc.enclosure},
				"media": map[string]any{
					"source":       map[string]any{"url": tc.source},
					"backupSource": map[string]any{"url": tc.backup, "mode": tc.mode},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			item, err := decodeItem(body, "episode")
			if err != nil {
				t.Fatal(err)
			}
			if item.MediaURL != tc.want {
				t.Fatalf("selected %q, want %q", item.MediaURL, tc.want)
			}
		})
	}
}
