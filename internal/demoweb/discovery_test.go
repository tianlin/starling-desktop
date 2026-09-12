package demoweb

import (
	"context"
	"starling/internal/model"
	"starling/internal/testkit"
	"testing"
)

func TestDiscoveryDemoCreatorSubscribeAndLibrary(t *testing.T) {
	d := newDiscoveryDemo(&testkit.Fake{})
	ctx := context.Background()
	users, e := d.Search(ctx, "token", "科学", model.SearchUser, "")
	if e != nil || len(users.Users) != 1 {
		t.Fatal(users, e)
	}
	creator, e := d.Creator(ctx, "token", users.Users[0].ID, "")
	if e != nil || len(creator.Items) != 9 {
		t.Fatal(creator, e)
	}
	pid := creator.Items[0].ID
	result, e := d.Subscribe(ctx, "token", pid)
	if e != nil || result.State != model.SubscriptionOn {
		t.Fatal(result, e)
	}
	page, e := d.List(ctx, "token", "subscriptions", "", "")
	if e != nil || len(page.Items) != 1 || page.Items[0].ID != pid {
		t.Fatal(page, e)
	}
	state, e := d.SubscriptionState(ctx, "token", pid)
	if e != nil || state.State != model.SubscriptionOn {
		t.Fatal(state, e)
	}
}
func TestDiscoveryDemoPagingAndNoInventedResults(t *testing.T) {
	d := newDiscoveryDemo(&testkit.Fake{})
	ctx := context.Background()
	p, e := d.Search(ctx, "token", "科学", model.SearchPodcast, "")
	if e != nil || p.Cursor == "" || len(p.Items) != 3 {
		t.Fatal(p, e)
	}
	next, e := d.Search(ctx, "token", "科学", model.SearchPodcast, p.Cursor)
	if e != nil || len(next.Items) != 3 || next.Items[0].ID == p.Items[0].ID {
		t.Fatal(next, e)
	}
	if _, e = d.Search(ctx, "token", "别的词", model.SearchPodcast, p.Cursor); e == nil {
		t.Fatal("unscoped cursor")
	}
	empty, e := d.Search(ctx, "token", "绝无匹配结果xyz", model.SearchPodcast, "")
	if e != nil || len(empty.Items) != 0 || !empty.Complete {
		t.Fatal(empty, e)
	}
	user, e := d.Creator(ctx, "token", discoveryUserID(1), "")
	if e != nil || len(user.Items) != 0 || !user.Complete {
		t.Fatal(user, e)
	}
}
