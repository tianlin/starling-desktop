package app

import (
	"context"
	"regexp"
	"starling/internal/model"
	"starling/internal/provider"
	"starling/internal/security"
)

var commentRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// IDs are retained until the account epoch changes, including failed/uncertain
// attempts. No comment text or response is retained in the deduplication ledger.
// Once full, reject new writes instead of evicting IDs that could be replayed.
const maxCommentAttempts = 4096

func (s *Service) CreateComment(ctx context.Context, epoch uint64, episodeID, text, requestID string) (model.CommentCreateResult, error) {
	var out model.CommentCreateResult
	if !security.ValidID(episodeID) {
		return out, model.Err("INVALID_ID", "单集 ID 无效。")
	}
	if !commentRequestID.MatchString(requestID) {
		return out, model.Err("INVALID_REQUEST", "发表请求标识无效。")
	}
	if err := model.ValidateCommentText(text); err != nil {
		return out, err
	}
	if err := s.checkExperimental(); err != nil {
		return out, err
	}
	p, ok := s.p.(provider.CommentWriter)
	if !ok {
		return out, model.Err("UNSUPPORTED", "当前连接不支持发表评论。")
	}
	if ctx.Err() != nil {
		return out, model.Err("CANCELLED", "请求尚未发送，操作已取消。")
	}
	snap := s.session.Snapshot()
	if snap.Epoch != epoch {
		return out, model.ErrStale
	}
	if snap.State != "connected" {
		return out, model.Err("UNAUTHORIZED", "请重新连接账号后发表评论。")
	}
	err := s.session.Commit(epoch, func(string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.commentEpoch != epoch {
			s.commentEpoch = epoch
			s.commentAttempts = map[string]struct{}{}
			s.commentBusy = false
		}
		if _, exists := s.commentAttempts[requestID]; exists {
			return model.Err("COMMENT_DUPLICATE", "该发表请求已处理或仍在处理中，请刷新评论核对；不会重复发送。")
		}
		if s.commentBusy {
			return model.Err("COMMENT_BUSY", "上一条评论仍在发表中，请等待结果。")
		}
		if len(s.commentAttempts) >= maxCommentAttempts {
			return model.Err("COMMENT_SESSION_LIMIT", "当前会话发表请求过多，请重新连接账号后再试。")
		}
		s.commentAttempts[requestID] = struct{}{}
		s.commentBusy = true
		return nil
	})
	if err != nil {
		return out, err
	}
	defer func() {
		s.mu.Lock()
		if s.commentEpoch == epoch {
			s.commentBusy = false
		}
		s.mu.Unlock()
	}()
	_, err = s.session.DoOnceAt(ctx, epoch, func(c context.Context, token string) error {
		comment, e := p.CreateComment(c, token, episodeID, text)
		if e != nil {
			return e
		}
		if !security.ValidID(comment.ID) || comment.Author.ID != snap.Identity.ID {
			return model.Err("COMMENT_UNCERTAIN", "发表结果未确认，请刷新评论或在官方客户端核对；不会自动重发。")
		}
		out.Comment = comment
		return nil
	})
	if err != nil {
		return model.CommentCreateResult{}, err
	}
	return out, nil
}
