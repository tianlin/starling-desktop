package demoweb

import (
	"fmt"
	"starling/internal/model"
	"strings"
	"time"
)

// Updates use their own IDs and pagination so older demo scenarios stay unchanged.
func updateSample(i int) model.Item {
	it := sample(i % 8)
	it.ID = fmt.Sprintf("64db2d493fa4090b744d%04d", i)
	it.Title = fmt.Sprintf("合成订阅更新 %02d · 在日常里寻找新发现", i+1)
	if i == 0 {
		it.Title += strings.Repeat("，这是一段用于验证窄窗口换行的长标题", 5)
	}
	day := time.Date(2026, 9, 12, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	it.Published = day.AddDate(0, 0, -(i / 20)).Add(-time.Duration(i%20) * time.Minute).Format(time.RFC3339)
	it.SourceURL = "https://www.xiaoyuzhoufm.com/episode/" + it.ID
	return it
}

func updatesPage(cursor string) (model.Page, error) {
	start, end, next := 0, 30, "updates-page-2"
	switch cursor {
	case "":
	case "updates-page-2":
		start, end, next = 29, 60, "updates-page-3"
	case "updates-page-3":
		start, end, next = 59, 85, ""
	default:
		return model.Page{}, model.Err("PAGINATION", "演示更新游标无效。")
	}
	items := make([]model.Item, 0, end-start)
	for i := start; i < end; i++ {
		items = append(items, updateSample(i))
	}
	return model.Page{Items: items, Cursor: next, Complete: next == ""}, nil
}
