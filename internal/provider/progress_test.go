package provider

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"starling/internal/model"
	"strings"
	"testing"
	"time"
)

const progressEID = "1234567890abcdef12345678"
const progressPID = "abcdef1234567890abcdef12"
const progressAt = "2026-09-13T04:00:00+08:00"
const progressRow = `{"eid":"` + progressEID + `","pid":"` + progressPID + `","progress":0,"playedAt":"` + progressAt + `"}`

func TestProgressReadContract(t *testing.T) {
	for _, data := range []string{"[]", "[" + progressRow + "]"} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string][]string
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 1 || len(body["eids"]) != 1 || body["eids"][0] != progressEID || r.Method != "POST" || r.URL.Path != "/v1/playback-progress/list" || r.Header.Get("x-jike-access-token") != "token" {
				t.Error("bad read contract")
			}
			_, _ = w.Write([]byte(`{"data":` + data + `}`))
		}))
		c := newClient(s.Client(), s.URL, s.URL, s.URL)
		rows, err := c.ReadProgress(context.Background(), "token", []string{progressEID})
		s.Close()
		if err != nil || rows == nil {
			t.Fatalf("rows=%v err=%v", rows, err)
		}
		if data == "[]" && len(rows) != 0 {
			t.Fatal(rows)
		}
		if data != "[]" && (len(rows) != 1 || rows[0].Position != 0 || rows[0].PlayedAt != progressAt) {
			t.Fatal(rows)
		}
	}
}
func TestProgressMalformedRead(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"data":null}`, `{"data":{}}`, `{"data":[{}]}`, `{"data":[` + progressRow + `,` + progressRow + `]}`, `{"data":[` + strings.Replace(progressRow, progressEID, progressPID, 1) + `]}`, `{"data":[` + strings.Replace(progressRow, `"progress":0`, `"progress":null`, 1) + `]}`, `{"data":[` + strings.Replace(progressRow, `"progress":0,`, ``, 1) + `]}`, `{"data":[` + strings.Replace(progressRow, progressAt, "yesterday", 1) + `]}`, `{"data":[` + strings.Replace(progressRow, `"progress":0`, `"progress":604801`, 1) + `]}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		c := newClient(s.Client(), s.URL, s.URL, s.URL)
		_, err := c.ReadProgress(context.Background(), "token", []string{progressEID})
		s.Close()
		if !model.IsCode(err, "BAD_RESPONSE") {
			t.Errorf("body %s err %v", body, err)
		}
	}
}
func TestProgressWriteContract(t *testing.T) {
	row := model.CloudProgress{EpisodeID: progressEID, PodcastID: progressPID, Position: 12.9, PlayedAt: progressAt}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Data []model.CloudProgress `json:"data"`
		}
		stamp, err := time.Parse(time.RFC3339, r.Header.Get("Local-Time"))
		if err != nil || time.Since(stamp) > 5*time.Second || time.Until(stamp) > 5*time.Second || r.Header.Get("Timezone") != "Asia/Shanghai" || r.UserAgent() != userAgent || r.Header.Get("x-jike-access-token") != "token" || r.Method != "POST" || r.URL.Path != "/v1/playback-progress/update" {
			t.Error("bad write headers")
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Data) != 1 || body.Data[0].Position != 12 || body.Data[0].PlayedAt != progressAt {
			t.Error("bad write body")
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer s.Close()
	c := newClient(s.Client(), s.URL, s.URL, s.URL)
	if err := c.WriteProgress(context.Background(), "token", []model.CloudProgress{row}); err != nil {
		t.Fatal(err)
	}
	if row.Position != 12.9 {
		t.Fatal("mutated caller")
	}
}
func TestProgressWriteFailuresSingleAttempt(t *testing.T) {
	for _, tt := range []struct {
		status     int
		body, code string
	}{{401, `{}`, "UNAUTHORIZED"}, {429, `{}`, "RATE_LIMIT"}, {503, `{}`, "UPSTREAM"}, {200, `null`, "BAD_RESPONSE"}, {200, `garbage`, "BAD_RESPONSE"}, {200, `{"success":false}`, "REQUEST_REJECTED"}} {
		calls := 0
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(tt.status)
			_, _ = w.Write([]byte(tt.body))
		}))
		c := newClient(s.Client(), s.URL, s.URL, s.URL)
		err := c.WriteProgress(context.Background(), "token", []model.CloudProgress{{EpisodeID: progressEID, PodcastID: progressPID, Position: 1, PlayedAt: progressAt}})
		s.Close()
		if !model.IsCode(err, tt.code) || calls != 1 {
			t.Errorf("err=%v calls=%d want=%s", err, calls, tt.code)
		}
	}
}
func TestProgressRejectsInvalidBeforeNetwork(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = w.Write([]byte(`{}`)) }))
	defer s.Close()
	c := newClient(s.Client(), s.URL, s.URL, s.URL)
	for _, position := range []float64{-1, 604801, math.NaN(), math.Inf(1)} {
		if c.WriteProgress(context.Background(), "token", []model.CloudProgress{{EpisodeID: progressEID, PodcastID: progressPID, Position: position, PlayedAt: progressAt}}) == nil {
			t.Fatal("accepted invalid position")
		}
	}
	for _, ids := range [][]string{{"../bad"}, {progressEID, progressEID}} {
		if _, err := c.ReadProgress(context.Background(), "token", ids); err == nil {
			t.Fatal("accepted invalid ids")
		}
	}
	if _, err := c.ReadProgress(context.Background(), "", []string{progressEID}); !model.IsCode(err, "UNAUTHORIZED") {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal(calls)
	}
}

func TestProgressReadAuthAndRateFailures(t *testing.T) {
	for _, tt := range []struct {
		status int
		code   string
	}{{401, "UNAUTHORIZED"}, {429, "RATE_LIMIT"}} {
		calls := 0
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(tt.status)
			_, _ = w.Write([]byte(`{}`))
		}))
		c := newClient(s.Client(), s.URL, s.URL, s.URL)
		_, err := c.ReadProgress(context.Background(), "token", []string{progressEID})
		s.Close()
		if !model.IsCode(err, tt.code) || calls != 1 {
			t.Fatalf("err=%v calls=%d", err, calls)
		}
	}
}

func TestProgressWriteInvalidRecordsAndEmpty(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = w.Write([]byte(`{}`)) }))
	defer s.Close()
	c := newClient(s.Client(), s.URL, s.URL, s.URL)
	valid := model.CloudProgress{EpisodeID: progressEID, PodcastID: progressPID, Position: 0, PlayedAt: progressAt}
	for _, field := range []string{"eid", "pid", "timestamp", "duplicate"} {
		row := valid
		switch field {
		case "eid":
			row.EpisodeID = "../bad"
		case "pid":
			row.PodcastID = ""
		case "timestamp":
			row.PlayedAt = "2026-09-13"
		}
		rows := []model.CloudProgress{row}
		if field == "duplicate" {
			rows = append(rows, row)
		}
		if err := c.WriteProgress(context.Background(), "token", rows); !model.IsCode(err, "INVALID_REQUEST") {
			t.Fatalf("%s: %v", field, err)
		}
	}
	if err := c.WriteProgress(context.Background(), "", []model.CloudProgress{valid}); !model.IsCode(err, "UNAUTHORIZED") {
		t.Fatal(err)
	}
	if err := c.WriteProgress(context.Background(), "token", nil); err != nil {
		t.Fatal(err)
	}
	if rows, err := c.ReadProgress(context.Background(), "token", nil); err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("%v %v", rows, err)
	}
	if calls != 0 {
		t.Fatal(calls)
	}
}
