package app

import (
	"context"
	"fmt"
	"starling/internal/model"
	"starling/internal/security"
	"time"
)

type libraryStage struct {
	view    model.LibraryView
	seen    map[string]bool
	loading bool
	cancel  context.CancelFunc
}

func cloneView(v model.LibraryView) model.LibraryView {
	v.Items = append([]model.Item{}, v.Items...)
	return v
}

func cachedView(v model.LibraryView) model.LibraryView {
	v = cloneView(v)
	v.Status = "cached"
	if at, e := time.Parse(time.RFC3339Nano, v.UpdatedAt); e != nil || time.Since(at) > 10*time.Minute {
		v.Status = "stale"
	}
	return v
}

// Library stages partial pages in memory. Only a proven end replaces the complete SQLite snapshot.
func (s *Service) Library(ctx context.Context, epoch uint64, kind, pid, mode string) (out model.LibraryView, resultErr error) {
	// Confirmed receipts augment views, never replace the complete library cache.
	defer func() {
		if kind != "subscriptions" || resultErr != nil {
			return
		}
		resultErr = s.session.Commit(epoch, func(scope string) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.overlaySubscriptionsLocked(scope, epoch, &out)
		})
	}()
	if kind == "updates" {
		return s.updates(ctx, epoch, mode)
	}
	if kind != "favorites" && kind != "subscriptions" && kind != "episodes" {
		return out, model.Err("UNSUPPORTED", "不支持的列表类型。")
	}
	if kind == "episodes" && !security.ValidID(pid) {
		return out, model.Err("INVALID_ID", "节目 ID 无效。")
	}
	snap := s.session.Snapshot()
	if snap.Epoch != epoch {
		return out, model.ErrStale
	}
	if kind != "episodes" && snap.State != "connected" {
		return out, model.Err("UNAUTHORIZED", "请先连接账号，再查看云端个人库。")
	}
	key := kind + ":" + pid
	stageKey := fmt.Sprintf("%d:%s", epoch, key)
	var stage *libraryStage
	var cursor string
	var revision uint64
	var requestCtx context.Context
	var requestCancel context.CancelFunc
	e := s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		stage = s.stages[stageKey]
		if mode == "cached" {
			if stage != nil {
				out = cloneView(stage.view)
				if out.Complete && out.Error == nil {
					out = cachedView(out)
				}
				return nil
			}
			cached := model.LibraryView{Items: []model.Item{}, Status: "idle", Epoch: epoch}
			ok, e := s.store.Get(scope, "cache", key, &cached)
			if e != nil {
				if kind == "subscriptions" {
					out = cached
					subscriptionCacheWarning(&out)
					return nil
				}
				return e
			}
			cached.Epoch = epoch
			if ok {
				cached = cachedView(cached)
			}
			out = cached
			return nil
		}
		if mode == "cancel" {
			if stage != nil {
				if stage.cancel != nil {
					stage.cancel()
				}
				stage.loading = false
				stage.view.Revision++
				if !stage.view.Complete {
					stage.view.Status = "partial"
				}
				out = cloneView(stage.view)
			} else {
				out = model.LibraryView{Items: []model.Item{}, Status: "idle", Epoch: epoch}
			}
			return nil
		}
		if mode != "refresh" && mode != "more" {
			return model.Err("UNSUPPORTED", "不支持的分页操作。")
		}
		if mode == "refresh" {
			if stage != nil && stage.loading {
				return model.Err("BUSY", "该列表正在加载，请勿重复刷新。")
			}
			s.revision++
			stage = &libraryStage{view: model.LibraryView{Items: []model.Item{}, Status: "loading", Revision: s.revision, Epoch: epoch}, seen: map[string]bool{}}
			s.stages[stageKey] = stage
		} else {
			if stage == nil {
				return model.Err("PAGINATION", "当前没有可继续的分页，请先刷新。")
			}
			if stage.loading {
				return model.Err("BUSY", "正在加载下一页。")
			}
			if stage.view.Cursor == "" {
				return model.Err("PAGINATION", "平台没有提供下一页游标。")
			}
			cursor = stage.view.Cursor
		}
		if stage.view.Pages >= 1000 || len(stage.view.Items) >= 20000 {
			return model.Err("PAGINATION", "已达到本轮安全上限，未标记为完整同步。")
		}
		revision = stage.view.Revision
		stage.loading = true
		requestCtx, requestCancel = context.WithCancel(ctx)
		stage.cancel = requestCancel
		return nil
	})
	if e != nil {
		return out, e
	}
	if mode == "cached" || mode == "cancel" {
		return out, nil
	}
	defer requestCancel()
	var page model.Page
	if snap.State == "connected" {
		_, e = s.session.Do(requestCtx, func(c context.Context, token string) error {
			var er error
			page, er = s.p.List(c, token, kind, pid, cursor)
			return er
		})
	} else {
		_, e = s.session.Guest(requestCtx, func(c context.Context) error { var er error; page, er = s.p.List(c, "", kind, pid, cursor); return er })
	}
	networkErr := e
	e = s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.stages[stageKey] != stage || stage.view.Revision != revision || !stage.loading {
			return model.ErrStale
		}
		stage.loading = false
		if networkErr == nil && page.Cursor != "" && (page.Cursor == cursor || stage.seen[page.Cursor] || len(page.Items) == 0) {
			networkErr = model.Err("PAGINATION", "检测到循环游标或异常空页，已停止继续加载。")
		}
		if networkErr == nil && page.Complete && page.Cursor != "" {
			networkErr = model.Err("PAGINATION", "平台分页结束标志互相矛盾。")
		}
		if networkErr != nil {
			stage.view.Status = "error"
			stage.view.Error = model.PublicError(networkErr)
			if len(stage.view.Items) == 0 {
				var cached model.LibraryView
				if ok, er := s.store.Get(scope, "cache", key, &cached); er != nil {
					if kind == "subscriptions" {
						out = cloneView(stage.view)
						subscriptionCacheWarning(&out)
						return nil
					}
					return er
				} else if ok {
					stage.view.Items = cached.Items
					stage.view.UpdatedAt = cached.UpdatedAt
				}
			}
			out = cloneView(stage.view)
			return nil
		}
		indexes := map[string]int{}
		for i, it := range stage.view.Items {
			indexes[it.Key()] = i
		}
		for _, raw := range page.Items {
			it, er := cleanItem(raw)
			if er != nil {
				return er
			}
			if i, ok := indexes[it.Key()]; ok {
				stage.view.Items[i] = it
			} else {
				indexes[it.Key()] = len(stage.view.Items)
				stage.view.Items = append(stage.view.Items, it)
			}
		}
		if page.Cursor != "" {
			stage.seen[page.Cursor] = true
		}
		stage.view.Cursor = page.Cursor
		stage.view.Complete = page.Complete
		stage.view.Pages++
		stage.view.UpdatedAt = model.Now()
		stage.view.Error = nil
		if page.Complete {
			stage.view.Status = "complete"
			if e := s.store.Put(scope, "cache", key, stage.view); e != nil {
				stage.view.Status = "error"
				stage.view.Error = model.PublicError(e)
			} else if kind == "subscriptions" {
				// Retire receipts only after a persisted complete authoritative refresh.
				for _, it := range stage.view.Items {
					if e := s.store.Delete(scope, "subscription-confirmed", it.ID); e != nil {
						break
					}
					if s.subscriptionEpoch == epoch {
						delete(s.subscriptionConfirmed, it.ID)
					}
				}
			}
		} else if page.Cursor != "" {
			stage.view.Status = "partial"
		} else {
			stage.view.Status = "unknown_end"
		}
		out = cloneView(stage.view)
		return nil
	})
	return out, e
}
