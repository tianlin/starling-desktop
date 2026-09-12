package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"starling/internal/model"
	"strings"
	"testing"
	"time"
)

const discoveryPID = "6013f9f58e2f7ee375cf4216"
const discoveryUID = "5fa391a5e0f5e723bbd34c78"

func TestDiscoveryWrappedUsersRejectRawOverflowAndEmptyContinuation(t *testing.T) {
	user := fmt.Sprintf(`{"uid":%q}`, discoveryUID)
	many := strings.TrimSuffix(strings.Repeat(user+",", 200), ",")
	for _, body := range []string{`{"data":[{"type":"SEARCHED_USERS","users":[` + many + `]},{"type":"SEARCHED_USERS","users":[` + many + `]}],"loadMoreKey":null}`, `{"data":[{"type":"SEARCHED_USERS","users":[]}],"loadMoreKey":{"loadMoreKey":20,"searchId":"synthetic"}}`} {
		c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, e := c.Search(context.Background(), "token", "用户", model.SearchUser, ""); e == nil {
			t.Fatal("accepted malformed user page")
		}
	}
}

func TestDiscoveryUserContinuationFromLiveContract(t *testing.T) {
	count := 0
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		count++
		if count == 1 {
			fmt.Fprintf(w, `{"data":[{"uid":%q,"nickname":"合成用户","type":"USER"}],"loadMoreKey":{"loadMoreKey":20,"searchId":"synthetic-user-search"}}`, discoveryUID)
		} else {
			fmt.Fprint(w, `{"data":[],"loadMoreKey":null}`)
		}
	})
	p, e := c.Search(context.Background(), "token", "科技", model.SearchUser, "")
	if e != nil || p.Cursor == "" || len(p.Users) != 1 {
		t.Fatal(p, e)
	}
	p, e = c.Search(context.Background(), "token", "科技", model.SearchUser, p.Cursor)
	if e != nil || !p.Complete || count != 2 {
		t.Fatal(p, e, count)
	}
}

func TestDiscoveryPodcastContinuationFromLiveContract(t *testing.T) {
	count := 0
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		count++
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		if count == 1 {
			fmt.Fprintf(w, `{"data":[{"pid":%q,"title":"合成节目"}],"loadMoreKey":{"loadMoreKey":20,"searchId":"synthetic-search"}}`, discoveryPID)
		} else {
			var key struct {
				Offset int    `json:"loadMoreKey"`
				ID     string `json:"searchId"`
			}
			_ = json.Unmarshal(body["loadMoreKey"], &key)
			if key.Offset != 20 || key.ID != "synthetic-search" {
				t.Fatal("continuation mismatch")
			}
			fmt.Fprint(w, `{"data":[],"loadMoreKey":null}`)
		}
	})
	p, e := c.Search(context.Background(), "token", "科技", model.SearchPodcast, "")
	if e != nil || p.Cursor == "" {
		t.Fatal(p, e)
	}
	p, e = c.Search(context.Background(), "token", "科技", model.SearchPodcast, p.Cursor)
	if e != nil || !p.Complete || count != 2 {
		t.Fatal(p, e, count)
	}
}

func TestDiscoveryQueryContract(t *testing.T) {
	for _, s := range []string{"", "  ", "a\n", strings.Repeat("中", 201)} {
		if _, e := model.NormalizeSearchQuery(s); !model.IsCode(e, "INVALID_REQUEST") {
			t.Fatalf("accepted %q: %v", s, e)
		}
	}
	if s, e := model.NormalizeSearchQuery(" 中 "); e != nil || s != "中" {
		t.Fatal(s, e)
	}
	if model.ValidateSearchKind(model.SearchKind("all")) == nil {
		t.Fatal("invalid kind accepted")
	}
}
func TestDiscoverySearchTypesAndScope(t *testing.T) {
	calls := 0
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		if r.URL.Path != "/v1/search/create" || b["keyword"] != "中文" {
			t.Error("request", r.URL, b)
		}
		switch b["type"] {
		case "PODCAST":
			fmt.Fprintf(w, `{"data":[{"pid":%q,"title":"节目","subscriptionStatus":"OFF"},{"pid":%q}],"hasMore":false}`, discoveryPID, discoveryPID)
		case "EPISODE":
			fmt.Fprintf(w, `{"data":[{"eid":%q,"media":{"url":"secret"},"podcast":{"pid":%q}}],"loadMoreKey":{"loadMoreKey":20,"searchId":"search-one"}}`, discoveryPID, discoveryPID)
		case "USER":
			fmt.Fprintf(w, `{"data":[{"type":"SEARCHED_USERS","users":[{"uid":%q,"nickname":"作者","avatar":{"picture":{"picUrl":"http://localhost/a"}}}]}],"hasMore":false}`, discoveryUID)
		default:
			t.Error(b)
		}
	})
	p, e := c.Search(context.Background(), "token", " 中文 ", model.SearchPodcast, "")
	if e != nil || len(p.Items) != 1 || !p.Complete || p.Subscriptions[discoveryPID] != model.SubscriptionOff {
		t.Fatal(p, e)
	}
	ep, e := c.Search(context.Background(), "token", "中文", model.SearchEpisode, "")
	if e != nil || ep.Cursor == "" || ep.Items[0].MediaURL != "" || ep.Subscriptions[discoveryPID] != model.SubscriptionUnknown {
		t.Fatal(ep, e)
	}
	for _, v := range []struct {
		q string
		k model.SearchKind
	}{{"different", model.SearchEpisode}, {"中文", model.SearchPodcast}} {
		_, e = c.Search(context.Background(), "token", v.q, v.k, ep.Cursor)
		if !model.IsCode(e, "BAD_CURSOR") {
			t.Fatal(e)
		}
	}
	u, e := c.Search(context.Background(), "token", "中文", model.SearchUser, "")
	if e != nil || len(u.Users) != 1 || u.Users[0].Avatar != "" || calls != 3 {
		t.Fatal(u, e, calls)
	}
}
func TestDiscoveryInvalidPages(t *testing.T) {
	for _, body := range []string{`{"data":null}`, `{"data":[],"hasMore":true}`, `{"data":[{"eid":"bad"}],"hasMore":false}`, `{"data":[{"eid":"6013f9f58e2f7ee375cf4216"}],"loadMoreKey":{"loadMoreKey":-1,"searchId":"s"}}`} {
		t.Run(body, func(t *testing.T) {
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
			if _, e := c.Search(context.Background(), "token", "q", model.SearchEpisode, ""); e == nil {
				t.Fatal("accepted")
			}
		})
	}
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":[]}`) })
	p, e := c.Search(context.Background(), "token", "q", model.SearchEpisode, "")
	if e != nil || p.Complete {
		t.Fatal(p, e)
	}
}
func TestDiscoveryCreatorAndSuggestions(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/search/get-preset":
			fmt.Fprint(w, `{"data":[{"text":" 中文 "},{"text":"中文"}]}`)
		case "/v1/profile/get":
			if r.URL.Query().Get("uid") != discoveryUID {
				t.Error(r.URL)
			}
			fmt.Fprintf(w, `{"data":{"uid":%q,"nickname":"作者","bio":"简介"}}`, discoveryUID)
		case "/v1/podcaster/owned-podcasts":
			fmt.Fprint(w, `{"data":[]}`)
		default:
			t.Error(r.URL)
		}
	})
	s, e := c.Suggestions(context.Background(), "token")
	if e != nil || len(s) != 1 {
		t.Fatal(s, e)
	}
	p, e := c.Creator(context.Background(), "token", discoveryUID, "")
	if e != nil || p.Creator.ID != discoveryUID || len(p.Items) != 0 || !p.Complete {
		t.Fatal(p, e)
	}
	if _, e = c.Creator(context.Background(), "token", discoveryUID, "cursor"); e == nil {
		t.Fatal("continued unsupported endpoint")
	}
}
func TestDiscoverySubscription(t *testing.T) {
	for _, tc := range []struct {
		name, body, code string
		status           int
	}{{"on", `{"data":{"pid":"6013f9f58e2f7ee375cf4216","subscriptionStatus":"ON"}}`, "", 200}, {"wrong id", `{"data":{"pid":"6013f9f58e2f7ee375cf4217","subscriptionStatus":"ON"}}`, "SUBSCRIPTION_UNCERTAIN", 200}, {"missing", `{"data":{"pid":"6013f9f58e2f7ee375cf4216"}}`, "SUBSCRIPTION_UNCERTAIN", 200}, {"invalid", "{", "SUBSCRIPTION_UNCERTAIN", 200}, {"auth", "{}", "UNAUTHORIZED", 401}, {"permission", "{}", "FORBIDDEN", 403}, {"rate", "{}", "RATE_LIMIT", 429}, {"server", "{}", "SUBSCRIPTION_UNCERTAIN", 503}} {
		t.Run(tc.name, func(t *testing.T) {
			n := 0
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				n++
				var b map[string]string
				json.NewDecoder(r.Body).Decode(&b)
				if r.URL.Path != "/v1/subscription/update" || b["mode"] != "ON" || b["pid"] != discoveryPID {
					t.Error(r.URL, b)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			p, e := c.Subscribe(context.Background(), "token", discoveryPID)
			if n != 1 || tc.code != "" && !model.IsCode(e, tc.code) || tc.code == "" && (e != nil || p.State != model.SubscriptionOn) {
				t.Fatal(p, e, n)
			}
		})
	}
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"data":{"pid":%q}}`, discoveryPID) })
	p, e := c.SubscriptionState(context.Background(), "token", discoveryPID)
	if e != nil || p.State != model.SubscriptionUnknown {
		t.Fatal(p, e)
	}
}
func TestDiscoverySubscriptionTimeout(t *testing.T) {
	n := 0
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		time.Sleep(50 * time.Millisecond)
		fmt.Fprint(w, `{}`)
	})
	c.http.Timeout = 10 * time.Millisecond
	_, e := c.Subscribe(context.Background(), "token", discoveryPID)
	if !model.IsCode(e, "SUBSCRIPTION_UNCERTAIN") || n != 1 {
		t.Fatal(e, n)
	}
}
func TestDiscoveryRejectsInputsBeforeNetwork(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected network request") })
	ctx := context.Background()
	for _, token := range []string{"", "bad\nheader"} {
		if _, e := c.Search(ctx, token, "q", model.SearchPodcast, ""); e == nil {
			t.Fatal("search token")
		}
		if _, e := c.Suggestions(ctx, token); e == nil {
			t.Fatal("suggestions token")
		}
		if _, e := c.Creator(ctx, token, discoveryUID, ""); e == nil {
			t.Fatal("creator token")
		}
		if _, e := c.Subscribe(ctx, token, discoveryPID); e == nil {
			t.Fatal("subscribe token")
		}
		if _, e := c.SubscriptionState(ctx, token, discoveryPID); e == nil {
			t.Fatal("state token")
		}
	}
	if _, e := c.Subscribe(ctx, "token", "../bad"); e == nil {
		t.Fatal("pid")
	}
	if _, e := c.Creator(ctx, "token", "bad", ""); e == nil {
		t.Fatal("uid")
	}
	if _, e := c.Search(ctx, "token", "q", model.SearchKind("ALL"), ""); e == nil {
		t.Fatal("kind")
	}
	if _, e := c.Search(ctx, "token", "q\n", model.SearchPodcast, ""); e == nil {
		t.Fatal("query")
	}
}
func TestDiscoveryAuthorIdentityAndOwnedPaging(t *testing.T) {
	for _, tc := range []struct{ profile, owned string }{{`{"data":{"uid":"6013f9f58e2f7ee375cf4216"}}`, `{"data":[]}`}, {`{"data":{"uid":"5fa391a5e0f5e723bbd34c78"}}`, `{"data":[],"hasMore":true}`}} {
		c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/profile/get" {
				fmt.Fprint(w, tc.profile)
			} else {
				fmt.Fprint(w, tc.owned)
			}
		})
		if _, e := c.Creator(context.Background(), "token", discoveryUID, ""); e == nil {
			t.Fatal("accepted invalid creator response")
		}
	}
}
func TestDiscoverySubscriptionReadIdentityAndStates(t *testing.T) {
	for _, tc := range []struct {
		body  string
		state model.SubscriptionState
		bad   bool
	}{{`{"data":{"pid":"6013f9f58e2f7ee375cf4217","subscriptionStatus":"ON"}}`, model.SubscriptionUnknown, true}, {`{"data":{"pid":"6013f9f58e2f7ee375cf4216","subscriptionStatus":"OFF"}}`, model.SubscriptionOff, false}, {`{"data":{"pid":"6013f9f58e2f7ee375cf4216","subscriptionStatus":"NEW_UNKNOWN_ENUM"}}`, model.SubscriptionUnknown, false}} {
		c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" || r.URL.Path != "/v1/podcast/get" || r.URL.Query().Get("pid") != discoveryPID {
				t.Error(r.URL)
			}
			fmt.Fprint(w, tc.body)
		})
		p, e := c.SubscriptionState(context.Background(), "token", discoveryPID)
		if (e != nil) != tc.bad || p.State != tc.state {
			t.Fatal(p, e)
		}
	}
}
func TestDiscoverySearchContinuationAndStalledCursor(t *testing.T) {
	var count int
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		count++
		var body map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		if count > 1 && string(body["loadMoreKey"]) != `{"loadMoreKey":20,"searchId":"search-one"}` {
			t.Error(string(body["loadMoreKey"]))
		}
		fmt.Fprintf(w, `{"data":[{"eid":%q}],"loadMoreKey":{"loadMoreKey":20,"searchId":"search-one"}}`, discoveryPID)
	})
	p, e := c.Search(context.Background(), "token", "q", model.SearchEpisode, "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Search(context.Background(), "token", "q", model.SearchEpisode, p.Cursor); !model.IsCode(e, "PAGINATION") {
		t.Fatal(e)
	}
}
func TestDiscoveryMetadataAndCaps(t *testing.T) {
	it, e := discoveryItem(json.RawMessage(`{"eid":"6013f9f58e2f7ee375cf4216","title":"A\u0000B","shownotes":"<iframe src='bad'>","media":{"url":"secret"},"image":{"picUrl":"http://127.0.0.1/private"}}`), "episode")
	if e != nil || it.Title != "AB" || it.MediaURL != "" || it.ShowNotes != "" || it.Image != "" {
		t.Fatal(it, e)
	}
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[`+strings.Repeat(`{"eid":"6013f9f58e2f7ee375cf4216"},`, 200)+`{"eid":"6013f9f58e2f7ee375cf4216"}],"hasMore":false}`)
	})
	if _, e := c.Search(context.Background(), "token", "q", model.SearchEpisode, ""); e == nil {
		t.Fatal("over limit accepted")
	}
}
func TestDiscoverySearchKeyRejectsControlAndExtraFields(t *testing.T) {
	for _, raw := range []string{`{"loadMoreKey":20,"searchId":"a\nb"}`, `{"loadMoreKey":20,"searchId":"x","unverified":true}`} {
		if validSearchKey(json.RawMessage(raw)) {
			t.Fatal("accepted unsafe search key")
		}
	}
}
