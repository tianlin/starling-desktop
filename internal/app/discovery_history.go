package app

import "starling/internal/model"

type discoveryTerms struct {
	Terms []string `json:"terms"`
}

func (s *Service) DiscoveryHistory(epoch uint64, mode, query string) (discoveryTerms, error) {
	out := discoveryTerms{Terms: []string{}}
	if mode == "record" || mode == "remove" {
		var err error
		query, err = model.NormalizeSearchQuery(query)
		if err != nil {
			return out, err
		}
	} else if mode != "read" && mode != "clear" {
		return out, model.Err("INVALID_REQUEST", "不支持的搜索历史操作。")
	} else if query != "" {
		return out, model.Err("INVALID_REQUEST", "此操作不接受搜索词。")
	}
	err := s.session.Commit(epoch, func(scope string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		terms := []string{}
		if _, err := s.store.Get(scope, "discovery", "history", &terms); err != nil {
			return err
		}
		if mode == "clear" {
			return s.store.Delete(scope, "discovery", "history")
		}
		if mode == "record" {
			out.Terms = append(out.Terms, query)
		}
		seen := map[string]bool{}
		if mode == "record" || mode == "remove" {
			seen[query] = true
		}
		for _, term := range terms {
			normalized, err := model.NormalizeSearchQuery(term)
			if err != nil || seen[normalized] {
				continue
			}
			seen[normalized] = true
			if len(out.Terms) < 10 {
				out.Terms = append(out.Terms, normalized)
			}
		}
		if mode != "read" {
			return s.store.Put(scope, "discovery", "history", out.Terms)
		}
		return nil
	})
	return out, err
}
