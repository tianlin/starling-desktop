package app

import (
	"context"
	"starling/internal/model"
	"starling/internal/provider"
	"starling/internal/security"
)

// Comments is deliberately memory-only: a failed page cannot replace a cached library.
func (s *Service) Comments(ctx context.Context, epoch uint64, episodeID, commentID, cursor string) (model.CommentPage, error) {
	return s.CommentsOrdered(ctx, epoch, episodeID, commentID, cursor, "")
}

func (s *Service) CommentsOrdered(ctx context.Context, epoch uint64, episodeID, commentID, cursor string, order model.CommentOrder) (model.CommentPage, error) {
	var page model.CommentPage
	order, orderErr := model.NormalizeCommentOrder(order)
	if orderErr != nil {
		return page, orderErr
	}
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
			if sorted, ok := s.p.(provider.SortedCommentReader); ok {
				page, e = sorted.CommentsOrdered(c, token, episodeID, cursor, order)
			} else if order == model.CommentOrderHot {
				page, e = p.Comments(c, token, episodeID, cursor)
			} else {
				e = model.Err("UNSUPPORTED", "当前连接不支持最新评论排序。")
			}
		} else {
			page, e = p.CommentThread(c, token, episodeID, commentID, cursor)
		}
		return e
	})
	if err != nil {
		return model.CommentPage{}, err
	}
	// Index only validated relationships from this account's completed reads.
	// No bodies or author information are retained in this write admission index.
	err = s.session.Commit(epoch, func(string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.commentReadEpoch != epoch {
			s.commentReadEpoch = epoch
			s.commentTargets = map[string]map[string]string{}
		}
		if s.commentTargets[episodeID] == nil {
			s.commentTargets[episodeID] = map[string]string{}
		}
		known := s.commentTargets[episodeID]
		for _, item := range page.Items {
			if !security.ValidID(item.ID) {
				continue
			}
			if commentID == "" && item.PrimaryCommentID == "" && item.ReplyTo == nil {
				known[item.ID] = item.ID
			} else if commentID != "" && item.PrimaryCommentID == commentID && item.ID != commentID {
				known[item.ID] = commentID
			}
		}
		return nil
	})
	if err != nil {
		return model.CommentPage{}, err
	}
	return page, nil
}
