package provider

import (
	"context"
	"io"
	"mime"
	"net/http"
	"os"
	"regexp"
	"starling/internal/model"
	"starling/internal/security"
	"strconv"
	"testing"
	"time"
)

// Manual anonymous probe only. Reads at most 17 response-body bytes per GET,
// never plays media, and never logs signed URLs, bodies, or raw network errors.
func TestLivePublicMediaRange(t *testing.T) {
	raw := os.Getenv("STARLING_LIVE_MEDIA_URL")
	if raw == "" {
		t.Skip("set STARLING_LIVE_MEDIA_URL to an authorized public episode share URL")
	}
	share, err := security.ParseShare(raw)
	if err != nil || share.Kind != "episode" {
		t.Fatal("expected an official public episode share URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	c := New()
	defer c.http.CloseIdleConnections()
	item, err := c.Detail(ctx, "", "episode", share.ID)
	if err != nil {
		failure := model.PublicError(err)
		t.Fatalf("anonymous detail failed: code=%s status=%d", failure.Code, failure.HTTPStatus)
	}
	if item.ID != share.ID || item.Restricted || item.MediaURL == "" {
		t.Fatal("public episode identity mismatch, restricted content, or missing media")
	}
	if security.ValidateMediaURL(item.MediaURL) != nil {
		t.Fatal("media URL rejected by the existing allowlist")
	}
	contentRange := regexp.MustCompile(`^bytes ([0-9]+)-([0-9]+)/([0-9]+)$`)
	total := int64(-1)
	for _, offset := range []int64{0, 1024} {
		end := offset + 15
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.MediaURL, nil)
		if err != nil {
			t.Fatal("could not construct anonymous media request")
		}
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-"+strconv.FormatInt(end, 10))
		req.Header.Set("Accept-Encoding", "identity")
		req.Header.Set("User-Agent", userAgent)
		// c.http retains the production public-IP dialer and refuses redirects.
		resp, err := c.http.Do(req)
		if err != nil {
			t.Fatalf("anonymous media transport failed: offset=%d timeout=%v", offset, ctx.Err() != nil)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 17))
		resp.Body.Close()
		kind, _, typeErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if typeErr != nil {
			kind = "unknown"
		}
		t.Logf("offset=%d status=%d type=%s bodyBytesRead=%d", offset, resp.StatusCode, kind, len(body))
		if readErr != nil {
			t.Fatal("bounded media body read failed")
		}
		if resp.StatusCode == http.StatusOK {
			t.Log("rangeSupported=false; full response was capped and closed")
			continue
		}
		if resp.StatusCode != http.StatusPartialContent {
			t.Fatal("unexpected media response status; redirect targets and response bodies withheld")
		}
		parts := contentRange.FindStringSubmatch(resp.Header.Get("Content-Range"))
		if len(parts) != 4 {
			t.Fatal("missing or malformed Content-Range")
		}
		values := make([]int64, 3)
		for i := range values {
			values[i], err = strconv.ParseInt(parts[i+1], 10, 64)
			if err != nil {
				t.Fatal("Content-Range number exceeds supported bounds")
			}
		}
		if values[0] != offset || values[1] != end || values[2] <= end || len(body) != 16 {
			t.Fatal("Content-Range or bounded response length does not match the request")
		}
		if total != -1 && total != values[2] {
			t.Fatal("media total length changed between range requests")
		}
		total = values[2]
		t.Logf("rangeSupported=true contentRange=bytes %d-%d/%d", values[0], values[1], total)
	}
}
