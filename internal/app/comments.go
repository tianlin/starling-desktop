package app

import (
	"context"
	"starling/internal/model"
	"starling/internal/provider"
	"starling/internal/security"
)

// Comments is deliberately memory-only: a failed page cannot replace a cached library.
func (s *Service) Comments(ctx context.Context, epoch uint64, episodeID, commentID, cursor string) (model.CommentPage, error) {
	var page model.CommentPage
	if !security.ValidID(episodeID) || (commentID != "" && !security.ValidID(commentID)) {
		return page, model.Err("INVALID_ID", "单集或评论 ID 无效。")
	}
	if _, err := provider.DecodeCursor(cursor); err != nil {
		return page, err
	}
	if err := s.checkExperimental(); err != nil {
		return page, err
	}
	p, ok := s.p.(provider.CommentReader)
	if !ok {
		return page, model.Err("UNSUPPORTED", "当前连接不支持评论阅读。")
	}
	_, err := s.session.DoAt(ctx, epoch, func(c context.Context, token string) error {
		var e error
		if commentID == "" {
			page, e = p.Comments(c, token, episodeID, cursor)
		} else {
			page, e = p.CommentThread(c, token, episodeID, commentID, cursor)
		}
		return e
	})
	if err != nil {
		return model.CommentPage{}, err
	}
	return page, nil
}
