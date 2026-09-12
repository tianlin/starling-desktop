package app

import (
	"context"
	"fmt"
	"sort"
	"starling/internal/model"
	"time"
)

// updates persists recent metadata after each successful page, while pagination
// state and the full accumulated list remain confined to the active session.
func (s *Service) updates(ctx context.Context, epoch uint64, mode string) (model.LibraryView, error) {
	var out model.LibraryView
	snap := s.session.Snapshot()
	if snap.Epoch != epoch {
		return out, model.ErrStale
	}
	if snap.State != "connected" {
		return out, model.Err("UNAUTHORIZED", "请先连接账号，再查看云端个人库。")
	}
	key := "updates:"
	stageKey := fmt.Sprintf("%d:%s", epoch, key)
	var stage *libraryStage
	var cursor string
	var revision uint64
	var requestCtx context.Context
	var cancel context.CancelFunc
	err := s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		stage = s.stages[stageKey]
		readCache := func() (model.LibraryView, error) {
			v := model.LibraryView{Items: []model.Item{}, Status: "idle", Epoch: epoch}
			ok, e := s.store.Get(scope, "cache", key, &v)
			v.Epoch = epoch
			v.Cursor = ""
			if ok {
				v = cachedView(v)
			}
			return v, e
		}
		if mode == "cached" {
			if stage != nil {
				out = cloneView(stage.view)
				if out.Complete && out.Error == nil {
					out = cachedView(out)
				}
				return nil
			}
			var e error
			out, e = readCache()
			return e
		}
		if mode == "cancel" {
			if stage == nil {
				var e error
				out, e = readCache()
				return e
			}
			if stage.cancel != nil {
				stage.cancel()
			}
			stage.loading = false
			stage.view.Revision++
			out = cloneView(stage.view)
			return nil
		}
		if mode != "refresh" && mode != "more" {
			return model.Err("UNSUPPORTED", "不支持的分页操作。")
		}
		if stage != nil && stage.loading {
			return model.Err("BUSY", "该列表正在加载，请稍后重试。")
		}
		if mode == "refresh" {
			previous := model.LibraryView{}
			seen := map[string]bool{}
			if stage != nil {
				previous = cloneView(stage.view)
				for k, v := range stage.seen {
					seen[k] = v
				}
			} else {
				var e error
				previous, e = readCache()
				if e != nil {
					return e
				}
			}
			s.revision++
			previous.Revision = s.revision
			stage = &libraryStage{view: previous, seen: seen}
			s.stages[stageKey] = stage
		} else {
			if stage == nil || stage.view.Cursor == "" {
				return model.Err("PAGINATION", "当前没有可继续的分页，请先刷新。")
			}
			if stage.view.Pages >= 1000 || len(stage.view.Items) >= 20000 {
				return model.Err("PAGINATION", "已达到本轮安全上限，未标记为完整同步。")
			}
			cursor = stage.view.Cursor
		}
		revision = stage.view.Revision
		stage.loading = true
		requestCtx, cancel = context.WithCancel(ctx)
		stage.cancel = cancel
		return nil
	})
	if err != nil || mode == "cached" || mode == "cancel" {
		return out, err
	}
	defer cancel()
	var page model.Page
	_, networkErr := s.session.DoAt(requestCtx, epoch, func(c context.Context, token string) error {
		var e error
		page, e = s.p.List(c, token, "updates", "", cursor)
		return e
	})
	err = s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.stages[stageKey] != stage || stage.view.Revision != revision || !stage.loading {
			return model.ErrStale
		}
		stage.loading = false
		fail := func(e error) error {
			stage.view.Status = "error"
			stage.view.Error = model.PublicError(e)
			out = cloneView(stage.view)
			return nil
		}
		if networkErr != nil {
			return fail(networkErr)
		}
		if page.Cursor != "" && (page.Cursor == cursor || (mode == "more" && stage.seen[page.Cursor]) || len(page.Items) == 0) {
			return fail(model.Err("PAGINATION", "检测到循环游标或异常空页，已停止继续加载。"))
		}
		if page.Complete && page.Cursor != "" {
			return fail(model.Err("PAGINATION", "平台分页结束标志互相矛盾。"))
		}
		// Validate every item before changing even one item in the visible stage.
		cleaned := make([]model.Item, 0, len(page.Items))
		for _, raw := range page.Items {
			it, e := cleanItem(raw)
			if e != nil {
				return fail(e)
			}
			if it.Kind != "episode" {
				return fail(model.Err("PARSE", "更新列表包含非单集内容。"))
			}
			cleaned = append(cleaned, it)
		}
		next := cloneView(stage.view)
		if mode == "refresh" {
			next.Items = []model.Item{}
			next.Pages = 0
		}
		indexes := map[string]int{}
		for i, it := range next.Items {
			indexes[it.Key()] = i
		}
		for _, it := range cleaned {
			if i, ok := indexes[it.Key()]; ok {
				next.Items[i] = it
			} else {
				indexes[it.Key()] = len(next.Items)
				next.Items = append(next.Items, it)
			}
		}
		if len(next.Items) > 20000 {
			return fail(model.Err("PAGINATION", "已达到本轮安全上限，未标记为完整同步。"))
		}
		sortUpdates(next.Items)
		next.Cursor = page.Cursor
		next.Complete = page.Complete
		next.Pages++
		next.UpdatedAt = model.Now()
		next.Error = nil
		if page.Complete {
			next.Status = "complete"
		} else if page.Cursor != "" {
			next.Status = "partial"
		} else {
			next.Status = "unknown_end"
		}
		snapshot := cloneView(next)
		snapshot.Cursor = ""
		snapshot.Status = "cached"
		if len(snapshot.Items) > 200 {
			snapshot.Items = snapshot.Items[:200]
			snapshot.Complete = false
		}
		if e := s.store.Put(scope, "cache", key, snapshot); e != nil {
			return fail(e)
		}
		if mode == "refresh" {
			stage.seen = map[string]bool{}
		}
		if page.Cursor != "" {
			stage.seen[page.Cursor] = true
		}
		stage.view = next
		out = cloneView(next)
		return nil
	})
	return out, err
}

func sortUpdates(items []model.Item) {
	sort.Slice(items, func(i, j int) bool {
		a, ae := time.Parse(time.RFC3339Nano, items[i].Published)
		b, be := time.Parse(time.RFC3339Nano, items[j].Published)
		if (ae == nil) != (be == nil) {
			return ae == nil
		}
		if ae == nil && !a.Equal(b) {
			return a.After(b)
		}
		return items[i].ID < items[j].ID
	})
}
