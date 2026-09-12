package demoweb

import "testing"

func TestUpdatesFixturePages(t *testing.T) {
	seen := map[string]bool{}
	cursor := ""
	duplicates := 0
	for page := 0; page < 3; page++ {
		p, err := updatesPage(cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range p.Items {
			if len(it.ID) != 24 {
				t.Fatal("invalid fixture ID", it.ID)
			}
			if seen[it.ID] {
				duplicates++
			}
			seen[it.ID] = true
			if it.MediaURL == "" || it.PodcastID == "" {
				t.Fatal("unplayable fixture", it.ID)
			}
		}
		cursor = p.Cursor
		if p.Complete != (page == 2) {
			t.Fatal("wrong completeness")
		}
	}
	if len(seen) != 85 || duplicates != 2 || cursor != "" {
		t.Fatalf("unique=%d duplicates=%d cursor=%s", len(seen), duplicates, cursor)
	}
	if _, err := updatesPage("invalid"); err == nil {
		t.Fatal("invalid cursor accepted")
	}
	if updateSample(0).Published == updateSample(40).Published {
		t.Fatal("missing date groups")
	}
}
