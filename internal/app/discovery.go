package app

import (
	"context"
	"starling/internal/model"
	"starling/internal/provider"
	"starling/internal/security"
)

func (s *Service) discoveryReader() (provider.DiscoveryReader, error) {
	if e := s.checkExperimental(); e != nil {
		return nil, e
	}
	p, ok := s.p.(provider.DiscoveryReader)
	if !ok {
		return nil, model.Err("UNSUPPORTED", "当前连接不支持探索，请使用小宇宙客户端。")
	}
	return p, nil
}

func (s *Service) DiscoverySuggestions(ctx context.Context, epoch uint64) (discoveryTerms, error) {
	out := discoveryTerms{Terms: []string{}}
	p, e := s.discoveryReader()
	if e != nil {
		return out, e
	}
	var terms []string
	_, e = s.session.DoAt(ctx, epoch, func(c context.Context, token string) error {
		var err error
		terms, err = p.Suggestions(c, token)
		return err
	})
	if e != nil {
		return out, e
	}
	seen := map[string]bool{}
	for _, term := range terms {
		q, err := model.NormalizeSearchQuery(term)
		if err == nil && !seen[q] && len(out.Terms) < 20 {
			seen[q] = true
			out.Terms = append(out.Terms, q)
		}
	}
	return out, nil
}

func (s *Service) DiscoverySearch(ctx context.Context, epoch, generation uint64, query string, kind model.SearchKind, cursor string) (model.SearchPage, error) {
	var out model.SearchPage
	query, e := model.NormalizeSearchQuery(query)
	if e != nil {
		return out, e
	}
	if e = model.ValidateSearchKind(kind); e != nil {
		return out, e
	}
	if generation == 0 || len(cursor) > 12000 {
		return out, model.Err("INVALID_REQUEST", "搜索请求无效。")
	}
	p, e := s.discoveryReader()
	if e != nil {
		return out, e
	}
	e = s.session.Commit(epoch, func(string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.searchEpoch != epoch {
			s.searchEpoch = epoch
			s.searchGeneration = 0
		}
		if generation <= s.searchGeneration {
			return model.ErrStale
		}
		s.searchGeneration = generation
		return nil
	})
	if e != nil {
		return out, e
	}
	_, e = s.session.DoAt(ctx, epoch, func(c context.Context, token string) error {
		var err error
		out, err = p.Search(c, token, query, kind, cursor)
		return err
	})
	if e != nil {
		return model.SearchPage{}, e
	}
	e = s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.searchEpoch != epoch || s.searchGeneration != generation {
			return model.ErrStale
		}
		return cleanDiscoveryPage(&out, kind, cursor)
	})
	if e != nil {
		return model.SearchPage{}, e
	}
	return out, nil
}

func cleanCreator(v model.Creator) (model.Creator, error) {
	if !security.ValidID(v.ID) || v.Nickname == "" {
		return model.Creator{}, model.Err("BAD_RESPONSE", "用户资料不完整。")
	}
	v.Avatar = security.ValidateImageURL(v.Avatar)
	v.Nickname = string([]rune(v.Nickname)[:min(200, len([]rune(v.Nickname)))])
	v.Bio = string([]rune(v.Bio)[:min(1000, len([]rune(v.Bio)))])
	return v, nil
}
func cleanDiscoveryPage(p *model.SearchPage, kind model.SearchKind, previousCursor string) error {
	if len(p.Items)+len(p.Users) > 200 || len(p.Cursor) > 12000 {
		return model.Err("BAD_RESPONSE", "搜索结果超出支持范围。")
	}
	if p.Cursor != "" && (p.Complete || p.Cursor == previousCursor || len(p.Items)+len(p.Users) == 0) {
		return model.Err("PAGINATION", "搜索分页状态无效。")
	}
	if (kind == model.SearchUser && len(p.Items) > 0) || (kind != model.SearchUser && len(p.Users) > 0) {
		return model.Err("BAD_RESPONSE", "搜索结果分类不匹配。")
	}
	items := []model.Item{}
	users := []model.Creator{}
	seen := map[string]bool{}
	subscriptions := map[string]model.SubscriptionState{}
	for _, raw := range p.Items {
		it, e := cleanItem(raw)
		if e != nil {
			return e
		}
		if it.Kind != string(kind) {
			return model.Err("BAD_RESPONSE", "搜索内容类型不匹配。")
		}
		if seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		items = append(items, it)
		if it.Kind == "podcast" {
			subscriptions[it.ID] = normalizeSubscriptionState(p.Subscriptions[it.ID])
		}
	}
	for _, raw := range p.Users {
		v, e := cleanCreator(raw)
		if e != nil {
			return e
		}
		if !seen[v.ID] {
			seen[v.ID] = true
			users = append(users, v)
		}
	}
	p.Items = items
	p.Users = users
	p.Subscriptions = subscriptions
	return nil
}

func (s *Service) DiscoveryCreator(ctx context.Context, epoch uint64, id, cursor string) (model.CreatorPage, error) {
	var out model.CreatorPage
	if !security.ValidID(id) || len(cursor) > 12000 {
		return out, model.Err("INVALID_REQUEST", "用户或分页参数无效。")
	}
	p, e := s.discoveryReader()
	if e != nil {
		return out, e
	}
	_, e = s.session.DoAt(ctx, epoch, func(c context.Context, token string) error {
		var err error
		out, err = p.Creator(c, token, id, cursor)
		return err
	})
	if e != nil {
		return model.CreatorPage{}, e
	}
	out.Creator, e = cleanCreator(out.Creator)
	if e != nil {
		return model.CreatorPage{}, e
	}
	if out.Creator.ID != id {
		return model.CreatorPage{}, model.Err("BAD_RESPONSE", "用户资料不匹配。")
	}
	page := model.SearchPage{Items: out.Items, Subscriptions: out.Subscriptions, Cursor: out.Cursor, Complete: out.Complete}
	if e = cleanDiscoveryPage(&page, model.SearchPodcast, cursor); e != nil {
		return model.CreatorPage{}, e
	}
	out.Items = page.Items
	out.Subscriptions = page.Subscriptions
	return out, nil
}
