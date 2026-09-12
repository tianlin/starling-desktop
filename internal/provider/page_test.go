package provider

import (
	"encoding/json"
	"testing"
)

func TestPageEnvelope(t *testing.T) {
	tests := []struct {
		json     string
		complete bool
		more     bool
		bad      bool
	}{
		{`{"data":[],"loadMoreKey":null}`, true, false, false},
		{`{"data":[{"eid":"64db2d493fa4090b744c313c"}],"loadMoreKey":{"skip":1}}`, false, true, false},
		{`{"data":[]}`, false, false, false},
		{`{"data":[],"hasMore":false}`, true, false, false},
		{`{"data":[],"loadMoreKey":{"skip":1}}`, false, false, true},
		{`{"data":null}`, false, false, true},
		{`{"error":"denied"}`, false, false, true},
		{`{"data":[],"hasMore":true}`, false, false, true},
		{`{"data":[],"loadMoreKey":null,"hasMore":true}`, false, false, true},
	}
	for _, tt := range tests {
		var env map[string]json.RawMessage
		json.Unmarshal([]byte(tt.json), &env)
		items, cursor, complete, err := DecodePage(env)
		_ = items
		if (err != nil) != tt.bad {
			t.Errorf("%s err=%v", tt.json, err)
			continue
		}
		if !tt.bad && (complete != tt.complete || (cursor != "") != tt.more) {
			t.Errorf("%s => %q %v", tt.json, cursor, complete)
		}
	}
}
func TestCursorRoundTrip(t *testing.T) {
	raw := json.RawMessage(`{"skip":20,"pubDate":"date"}`)
	c := EncodeCursor(raw)
	v, e := DecodeCursor(c)
	if e != nil {
		t.Fatal(e)
	}
	var a, b any
	json.Unmarshal(raw, &a)
	json.Unmarshal(v, &b)
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if string(aa) != string(bb) {
		t.Fatal(a, b)
	}
	if _, e = DecodeCursor("not-json"); e == nil {
		t.Fatal("bad cursor")
	}
}
