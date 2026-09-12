package provider

import (
	"context"
	"fmt"
	"net/http"
	"starling/internal/model"
	"testing"
)

func TestPublicEpisodePreviewShapes(t *testing.T) {
	const episode = `{"eid":"64db2d493fa4090b744c313c","title":"preview"}`
	for _, tc := range []struct {
		name, props, code string
		count             int
	}{
		{"nested", `{"podcast":{"episodes":[` + episode + `]}}`, "", 1},
		{"top-level", `{"episodes":[` + episode + `]}`, "", 1},
		{"empty-preview", `{"podcast":{"episodes":[]}}`, "", 0},
		{"missing", `{"podcast":{}}`, "PUBLIC_UNAVAILABLE", 0},
		{"null-preview", `{"podcast":{"episodes":null}}`, "BAD_RESPONSE", 0},
		{"object-preview", `{"podcast":{"episodes":{}}}`, "BAD_RESPONSE", 0},
		{"invalid-podcast", `{"podcast":[]}`, "BAD_RESPONSE", 0},
		{"invalid-top-not-masked", `{"episodes":null,"podcast":{"episodes":[` + episode + `]}}`, "BAD_RESPONSE", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("x-jike-access-token") != "" {
					t.Error("anonymous preview sent account token")
				}
				fmt.Fprintf(w, `<script id="__NEXT_DATA__">{"props":{"pageProps":%s}}</script>`, tc.props)
			})
			page, err := c.List(context.Background(), "", "episodes", "64db2d493fa4090b744c313d", "")
			if tc.code != "" {
				if !model.IsCode(err, tc.code) {
					t.Fatalf("expected %s, got %v", tc.code, err)
				}
				return
			}
			if err != nil || len(page.Items) != tc.count || page.Complete || page.Cursor != "" {
				t.Fatalf("preview=%+v error=%v", page, err)
			}
		})
	}
}
