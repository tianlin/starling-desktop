package app

import (
	"context"
	"encoding/json"
	"fmt"
	"starling/internal/model"
	"starling/internal/provider"
	"starling/internal/security"
)

func normalizeSubscriptionState(state model.SubscriptionState) model.SubscriptionState {
	if state == model.SubscriptionOn || state == model.SubscriptionOff {
		return state
	}
	return model.SubscriptionUnknown
}
func cleanSubscriptionResult(v model.SubscriptionResult, id string) (model.SubscriptionResult, error) {
	if v.PodcastID != id {
		return model.SubscriptionResult{}, model.Err("SUBSCRIPTION_UNCERTAIN", "返回的节目不匹配，订阅结果待确认。")
	}
	v.State = normalizeSubscriptionState(v.State)
	if v.Item != nil {
		it, e := cleanItem(*v.Item)
		if e != nil || it.Kind != "podcast" || it.ID != id {
			return model.SubscriptionResult{}, model.Err("SUBSCRIPTION_UNCERTAIN", "节目资料不匹配，订阅结果待确认。")
		}
		v.Item = &it
	}
	return v, nil
}

func (s *Service) SubscriptionStatus(ctx context.Context, epoch uint64, id string) (model.SubscriptionResult, error) {
	if !security.ValidID(id) {
		return model.SubscriptionResult{}, model.Err("INVALID_ID", "节目 ID 无效。")
	}
	if e := s.checkExperimental(); e != nil {
		return model.SubscriptionResult{}, e
	}
	p, ok := s.p.(provider.SubscriptionReader)
	if !ok {
		return model.SubscriptionResult{}, model.Err("UNSUPPORTED", "当前连接不支持读取订阅状态。")
	}
	var result model.SubscriptionResult
	_, e := s.session.DoAt(ctx, epoch, func(c context.Context, token string) error {
		var err error
		result, err = p.SubscriptionState(c, token, id)
		return err
	})
	if e != nil {
		return model.SubscriptionResult{}, e
	}
	result, e = cleanSubscriptionResult(result, id)
	if e != nil {
		return model.SubscriptionResult{}, e
	}
	// Resolving an earlier uncertain add must also repair the library overlay.
	if result.State == model.SubscriptionOn && result.Item != nil {
		e = s.session.Commit(epoch, func(scope string) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.subscriptionEpoch == epoch && s.subscriptionPending[id] {
				return s.confirmSubscriptionLocked(scope, epoch, result)
			}
			return nil
		})
	}
	if model.IsCode(e, "SUBSCRIPTION_LOCAL_CACHE") {
		result.Warning = model.PublicError(e)
		e = nil
	}
	return result, e
}

func (s *Service) AddSubscription(ctx context.Context, epoch uint64, id, requestID string) (model.SubscriptionResult, error) {
	var result model.SubscriptionResult
	if !security.ValidID(id) {
		return result, model.Err("INVALID_ID", "节目 ID 无效。")
	}
	if !commentRequestID.MatchString(requestID) {
		return result, model.Err("INVALID_REQUEST", "订阅请求标识无效。")
	}
	if e := s.checkExperimental(); e != nil {
		return result, e
	}
	p, ok := s.p.(provider.SubscriptionWriter)
	if !ok {
		return result, model.Err("UNSUPPORTED", "当前连接不支持订阅节目。")
	}
	snap := s.session.Snapshot()
	if snap.Epoch != epoch {
		return result, model.ErrStale
	}
	if snap.State != "connected" {
		return result, model.Err("UNAUTHORIZED", "请连接账号后订阅节目。")
	}
	if ctx.Err() != nil {
		return result, model.Err("CANCELLED", "请求尚未发送。")
	}
	e := s.session.Commit(epoch, func(string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.subscriptionEpoch != epoch {
			s.subscriptionEpoch = epoch
			s.subscriptionAttempts = map[string]bool{}
			s.subscriptionBusy = map[string]bool{}
			s.subscriptionPending = map[string]bool{}
			s.subscriptionConfirmed = map[string]model.Item{}
		}
		if s.subscriptionAttempts[requestID] {
			return model.Err("SUBSCRIPTION_DUPLICATE", "该订阅请求已处理，请核对订阅状态。")
		}
		if s.subscriptionBusy[id] {
			return model.Err("SUBSCRIPTION_BUSY", "该节目正在订阅，请等待结果。")
		}
		if len(s.subscriptionAttempts) >= 4096 {
			return model.Err("SUBSCRIPTION_LIMIT", "当前会话操作过多，请重新连接后再试。")
		}
		s.subscriptionAttempts[requestID] = true
		s.subscriptionBusy[id] = true
		s.subscriptionPending[id] = true
		return nil
	})
	if e != nil {
		return result, e
	}
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.subscriptionEpoch == epoch {
			delete(s.subscriptionBusy, id)
		}
	}()
	_, e = s.session.DoOnceAt(ctx, epoch, func(c context.Context, token string) error {
		var err error
		result, err = p.Subscribe(c, token, id)
		if err != nil {
			return err
		}
		result, err = cleanSubscriptionResult(result, id)
		if err != nil {
			return err
		}
		if result.State != model.SubscriptionOn {
			return model.Err("SUBSCRIPTION_UNCERTAIN", "订阅结果待确认，请先核对状态。")
		}
		return nil
	})
	if model.IsCode(e, "SUBSCRIPTION_UNCERTAIN") {
		// Read once after an ambiguous transport result; never repeat the write.
		verified, readErr := s.SubscriptionStatus(ctx, epoch, id)
		if readErr == nil && verified.State == model.SubscriptionOn {
			result = verified
			e = nil
		}
	}
	if e != nil {
		return model.SubscriptionResult{}, e
	}
	if result.Item == nil {
		it, readErr := s.Detail(ctx, epoch, "podcast", id)
		if readErr != nil {
			return model.SubscriptionResult{}, model.Err("SUBSCRIPTION_UNCERTAIN", "已发送订阅，节目资料暂时无法读取，请核对状态。")
		}
		result.Item = &it
	}
	e = s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.confirmSubscriptionLocked(scope, epoch, result)
	})
	if model.IsCode(e, "SUBSCRIPTION_LOCAL_CACHE") {
		result.Warning = model.PublicError(e)
		e = nil
	}
	return result, e
}

// Caller holds the account commit lock and service mutex. Confirmed additions
// are independent receipts, never a replacement of the complete library cache.
func (s *Service) confirmSubscriptionLocked(scope string, epoch uint64, result model.SubscriptionResult) error {
	if result.Item == nil {
		return nil
	}
	it, e := cleanItem(*result.Item)
	if e != nil {
		return e
	}
	if s.subscriptionEpoch != epoch {
		s.subscriptionEpoch = epoch
		s.subscriptionConfirmed = map[string]model.Item{}
		s.subscriptionPending = map[string]bool{}
		s.subscriptionAttempts = map[string]bool{}
		s.subscriptionBusy = map[string]bool{}
	}
	if s.subscriptionConfirmed == nil {
		s.subscriptionConfirmed = map[string]model.Item{}
	}
	s.subscriptionConfirmed[it.ID] = it
	delete(s.subscriptionPending, it.ID)
	for _, kind := range []string{"subscriptions:", "updates:"} {
		key := fmt.Sprintf("%d:%s", epoch, kind)
		if stage := s.stages[key]; stage != nil {
			if stage.cancel != nil {
				stage.cancel()
			}
			delete(s.stages, key)
		}
	}
	// A local storage error does not undo a confirmed upstream subscription.
	// The in-memory receipt remains usable and the result carries a warning.
	if e = s.store.Put(scope, "subscription-confirmed", it.ID, it); e != nil {
		return model.Err("SUBSCRIPTION_LOCAL_CACHE", "已订阅成功，但本机缓存保存失败；刷新后可重新读取。")
	}
	return nil
}

func (s *Service) overlaySubscriptionsLocked(scope string, epoch uint64, v *model.LibraryView) error {
	rows, e := s.store.List(scope, "subscription-confirmed")
	if e != nil {
		subscriptionCacheWarning(v)
	}
	receipts := map[string]model.Item{}
	for _, row := range rows {
		var it model.Item
		if json.Unmarshal(row, &it) != nil {
			subscriptionCacheWarning(v)
			continue
		}
		it, e = cleanItem(it)
		if e != nil || it.Kind != "podcast" {
			subscriptionCacheWarning(v)
			continue
		}
		receipts[it.ID] = it
	}
	if s.subscriptionEpoch == epoch {
		for id, it := range s.subscriptionConfirmed {
			receipts[id] = it
		}
	}
	seen := map[string]bool{}
	for _, it := range v.Items {
		seen[it.ID] = true
	}
	// Stable ID order avoids jumps when several adds await platform propagation.
	additions := []model.Item{}
	for _, it := range receipts {
		if !seen[it.ID] {
			additions = append(additions, it)
		}
	}
	sortSubscriptionItems(additions)
	v.Items = append(additions, v.Items...)
	if len(additions) > 0 && v.Status == "idle" {
		v.Status = "partial"
	}
	return nil
}

func subscriptionCacheWarning(v *model.LibraryView) {
	v.Status = "error"
	if v.Error == nil {
		v.Error = &model.AppError{Code: "SUBSCRIPTION_LOCAL_CACHE", Message: "本机订阅缓存暂不可用，已保留当前可用结果；可稍后刷新。"}
	}
}

func sortSubscriptionItems(items []model.Item) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].ID < items[j-1].ID; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
