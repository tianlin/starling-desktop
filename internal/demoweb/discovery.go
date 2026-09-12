package demoweb

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"starling/internal/model"
	"starling/internal/testkit"
	"strconv"
	"strings"
	"sync"
)

// This fixture is linked only into the synthetic demo, never the desktop host.
type discoveryDemo struct {
	*testkit.Fake
	mu         sync.Mutex
	subscribed map[string]bool
}

func newDiscoveryDemo(f *testkit.Fake) *discoveryDemo {
	return &discoveryDemo{Fake: f, subscribed: map[string]bool{}}
}
func discoveryPodcast(i int) model.Item {
	id := fmt.Sprintf("64db2d493fa4090b744c51%02d", i)
	return model.Item{Kind: "podcast", ID: id, Title: fmt.Sprintf("合成探索节目 %02d · 科学与生活", i+1), Description: "合成演示内容 · 和科学主播一起发现生活中的新问题。只用于功能验收，不是真实播客。", SourceURL: "https://www.xiaoyuzhoufm.com/podcast/" + id}
}
func discoveryUserID(i int) string { return fmt.Sprintf("64db2d493fa4090b744c61%02d", i) }
func discoveryUsers() []model.Creator {
	return []model.Creator{{ID: discoveryUserID(0), Nickname: "合成科学主播", Bio: "合成创作者 · 这里有关于科学和生活的节目。"}, {ID: discoveryUserID(1), Nickname: "合成普通听友", Bio: "喜欢城市、散步和认真听故事。"}}
}
func (d *discoveryDemo) Suggestions(context.Context, string) ([]string, error) {
	return []string{"科学", "生活", "城市", "合成科学主播"}, nil
}
func (d *discoveryDemo) Search(ctx context.Context, token, query string, kind model.SearchKind, cursor string) (model.SearchPage, error) {
	out := model.SearchPage{Items: []model.Item{}, Users: []model.Creator{}, Subscriptions: map[string]model.SubscriptionState{}}
	q, e := model.NormalizeSearchQuery(query)
	if e != nil {
		return out, e
	}
	if e = model.ValidateSearchKind(kind); e != nil {
		return out, e
	}
	scope := fmt.Sprintf("%x", sha256.Sum256([]byte(string(kind)+":"+q)))
	offset := 0
	if cursor != "" {
		parts := strings.Split(cursor, ":")
		if len(parts) != 2 || parts[0] != scope {
			return out, model.Err("BAD_CURSOR", "演示搜索游标不匹配。")
		}
		offset, e = strconv.Atoi(parts[1])
		if e != nil || offset < 0 {
			return out, model.Err("BAD_CURSOR", "演示搜索游标无效。")
		}
	}
	match := func(text string) bool { return strings.Contains(strings.ToLower(text), strings.ToLower(q)) }
	if kind == model.SearchUser {
		for _, u := range discoveryUsers() {
			if match(u.Nickname + " " + u.Bio) {
				out.Users = append(out.Users, u)
			}
		}
	} else if kind == model.SearchPodcast {
		for i := 0; i < 9; i++ {
			it := discoveryPodcast(i)
			if match(it.Title + " " + it.Description) {
				out.Items = append(out.Items, it)
			}
		}
		for i := 0; i < 4; i++ {
			if match(programs[i]) {
				it := sample(i)
				it.Kind = "podcast"
				it.ID = podcastID(i)
				it.Title = programs[i]
				it.SourceURL = "https://www.xiaoyuzhoufm.com/podcast/" + it.ID
				out.Items = append(out.Items, it)
			}
		}
	} else {
		for i := 0; i < 8; i++ {
			it := sample(i)
			if match(it.Title + " " + it.Description + " 科学 生活") {
				out.Items = append(out.Items, it)
			}
		}
	}
	total := len(out.Items) + len(out.Users)
	if offset > total {
		return out, model.Err("BAD_CURSOR", "演示搜索游标超出范围。")
	}
	end := min(offset+3, total)
	if kind == model.SearchUser {
		out.Users = out.Users[offset:end]
	} else {
		out.Items = out.Items[offset:end]
	}
	out.Complete = end == total
	if !out.Complete {
		out.Cursor = fmt.Sprintf("%s:%d", scope, end)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, it := range out.Items {
		if it.Kind == "podcast" {
			out.Subscriptions[it.ID] = d.stateLocked(it.ID)
		}
	}
	return out, nil
}
func (d *discoveryDemo) stateLocked(id string) model.SubscriptionState {
	if d.subscribed[id] {
		return model.SubscriptionOn
	}
	for i := 0; i < 4; i++ {
		if id == podcastID(i) {
			return model.SubscriptionOn
		}
	}
	return model.SubscriptionOff
}
func (d *discoveryDemo) Creator(ctx context.Context, token, id, cursor string) (model.CreatorPage, error) {
	out := model.CreatorPage{Items: []model.Item{}, Subscriptions: map[string]model.SubscriptionState{}, Complete: true}
	if cursor != "" {
		return out, model.Err("BAD_CURSOR", "演示用户作品不分页。")
	}
	found := false
	for _, u := range discoveryUsers() {
		if id == u.ID {
			out.Creator = u
			found = true
		}
	}
	if !found {
		return out, model.Err("NOT_FOUND", "演示用户不存在。")
	}
	if id == discoveryUserID(0) {
		d.mu.Lock()
		defer d.mu.Unlock()
		for i := 0; i < 9; i++ {
			it := discoveryPodcast(i)
			out.Items = append(out.Items, it)
			out.Subscriptions[it.ID] = d.stateLocked(it.ID)
		}
	}
	return out, nil
}
func (d *discoveryDemo) SubscriptionState(ctx context.Context, token, id string) (model.SubscriptionResult, error) {
	it, e := d.Detail(ctx, token, "podcast", id)
	if e != nil {
		return model.SubscriptionResult{}, e
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return model.SubscriptionResult{PodcastID: id, State: d.stateLocked(id), Item: &it}, nil
}
func (d *discoveryDemo) Subscribe(ctx context.Context, token, id string) (model.SubscriptionResult, error) {
	it, e := d.Detail(ctx, token, "podcast", id)
	if e != nil {
		return model.SubscriptionResult{}, e
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.subscribed[id] = true
	return model.SubscriptionResult{PodcastID: id, State: model.SubscriptionOn, Item: &it}, nil
}
func (d *discoveryDemo) Detail(ctx context.Context, token, kind, id string) (model.Item, error) {
	if kind == "podcast" {
		for i := 0; i < 9; i++ {
			it := discoveryPodcast(i)
			if it.ID == id {
				return it, nil
			}
		}
	}
	return d.Fake.Detail(ctx, token, kind, id)
}
func (d *discoveryDemo) List(ctx context.Context, token, kind, pid, cursor string) (model.Page, error) {
	if kind == "episodes" {
		for i := 0; i < 9; i++ {
			if discoveryPodcast(i).ID == pid {
				items := []model.Item{}
				for j := 0; j < 3; j++ {
					items = append(items, sample(j))
				}
				return model.Page{Items: items, Complete: true}, nil
			}
		}
	}
	p, e := d.Fake.List(ctx, token, kind, pid, cursor)
	if e != nil || kind != "subscriptions" || !p.Complete {
		return p, e
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	ids := []string{}
	for id := range d.subscribed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for i := 0; i < 9; i++ {
			it := discoveryPodcast(i)
			if it.ID == id {
				p.Items = append(p.Items, it)
			}
		}
	}
	return p, nil
}
