// Package testkit is used by tests and the explicitly labelled demonstration host only.
package testkit

import (
	"context"
	"starling/internal/model"
	"sync"
)

type Fake struct {
	SendFunc            func(context.Context, string, string) error
	LoginFunc           func(context.Context, string, string, string) (model.Credentials, model.Identity, error)
	MeFunc              func(context.Context, string) (model.Identity, error)
	RefreshFunc         func(context.Context, model.Credentials) (model.Credentials, error)
	ListFunc            func(context.Context, string, string, string, string) (model.Page, error)
	DetailFunc          func(context.Context, string, string, string) (model.Item, error)
	CommentsFunc        func(context.Context, string, string, string) (model.CommentPage, error)
	CommentsOrderedFunc func(context.Context, string, string, string, model.CommentOrder) (model.CommentPage, error)
	CommentThreadFunc   func(context.Context, string, string, string, string) (model.CommentPage, error)
	CreateCommentFunc   func(context.Context, string, string, string) (model.Comment, error)
	ReplyCommentFunc    func(context.Context, string, string, string, string, string) (model.Comment, error)
}

func (f *Fake) ReplyComment(ctx context.Context, token, eid, text, target, primary string) (model.Comment, error) {
	if f.ReplyCommentFunc != nil {
		return f.ReplyCommentFunc(ctx, token, eid, text, target, primary)
	}
	return model.Comment{}, model.Err("UNSUPPORTED", "合成提供方未配置回复评论。")
}

func (f *Fake) CreateComment(ctx context.Context, token, eid, text string) (model.Comment, error) {
	if f.CreateCommentFunc != nil {
		return f.CreateCommentFunc(ctx, token, eid, text)
	}
	return model.Comment{}, model.Err("UNSUPPORTED", "合成提供方未配置发表评论。")
}

func (f *Fake) Comments(ctx context.Context, token, eid, cursor string) (model.CommentPage, error) {
	if f.CommentsFunc != nil {
		return f.CommentsFunc(ctx, token, eid, cursor)
	}
	return model.CommentPage{Items: []model.Comment{}, Complete: true}, nil
}
func (f *Fake) CommentsOrdered(ctx context.Context, token, eid, cursor string, order model.CommentOrder) (model.CommentPage, error) {
	if f.CommentsOrderedFunc != nil {
		return f.CommentsOrderedFunc(ctx, token, eid, cursor, order)
	}
	return f.Comments(ctx, token, eid, cursor)
}
func (f *Fake) CommentThread(ctx context.Context, token, eid, cid, cursor string) (model.CommentPage, error) {
	if f.CommentThreadFunc != nil {
		return f.CommentThreadFunc(ctx, token, eid, cid, cursor)
	}
	return model.CommentPage{Items: []model.Comment{}, Complete: true}, nil
}

func (f *Fake) SendCode(c context.Context, p, a string) error {
	if f.SendFunc != nil {
		return f.SendFunc(c, p, a)
	}
	return nil
}
func (f *Fake) Login(c context.Context, p, a, v string) (model.Credentials, model.Identity, error) {
	if f.LoginFunc != nil {
		return f.LoginFunc(c, p, a, v)
	}
	return model.Credentials{Access: "old", Refresh: "refresh-old"}, model.Identity{ID: "user-a", Nickname: "合成测试账号"}, nil
}
func (f *Fake) Me(c context.Context, t string) (model.Identity, error) {
	if f.MeFunc != nil {
		return f.MeFunc(c, t)
	}
	return model.Identity{ID: "user-a", Nickname: "合成测试账号"}, nil
}
func (f *Fake) Refresh(c context.Context, t model.Credentials) (model.Credentials, error) {
	if f.RefreshFunc != nil {
		return f.RefreshFunc(c, t)
	}
	return model.Credentials{Access: "new", Refresh: "refresh-new"}, nil
}
func (f *Fake) List(c context.Context, t, k, p, cur string) (model.Page, error) {
	if f.ListFunc != nil {
		return f.ListFunc(c, t, k, p, cur)
	}
	return model.Page{Items: []model.Item{}, Complete: true}, nil
}
func (f *Fake) Detail(c context.Context, t, k, id string) (model.Item, error) {
	if f.DetailFunc != nil {
		return f.DetailFunc(c, t, k, id)
	}
	return model.Item{Kind: k, ID: id, Title: "合成测试单集", SourceURL: "https://www.xiaoyuzhoufm.com/" + k + "/" + id, MediaURL: "https://media.xyzcdn.net/fixture.wav", Duration: 120}, nil
}

type MemoryVault struct {
	mu                sync.Mutex
	Value             model.SavedSession
	SaveErr, ClearErr error
	Saves             int
}

func (v *MemoryVault) Available() bool { return true }
func (v *MemoryVault) Save(s model.SavedSession) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.SaveErr != nil {
		return v.SaveErr
	}
	v.Value = s
	v.Saves++
	return nil
}
func (v *MemoryVault) Load() (model.SavedSession, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.Value, nil
}
func (v *MemoryVault) Clear() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.ClearErr != nil {
		return v.ClearErr
	}
	v.Value = model.SavedSession{}
	return nil
}
