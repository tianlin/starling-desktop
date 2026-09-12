package app

import (
	"bytes"
	"context"
	"encoding/json"
	qrcode "github.com/skip2/go-qrcode"
	"io"
	"runtime"
	"starling/internal/model"
)

type reply struct {
	OK    bool            `json:"ok"`
	Data  any             `json:"data,omitempty"`
	Error *model.AppError `json:"error,omitempty"`
}
type diagnosticEvent struct {
	Time         string `json:"time"`
	Action       string `json:"action"`
	Code         string `json:"code"`
	HTTPStatus   int    `json:"httpStatus,omitempty"`
	UpstreamCode int    `json:"upstreamCode,omitempty"`
}

func decodePayload(raw string, v any) error {
	if len(raw) > 1<<20 {
		return model.Err("INVALID_REQUEST", "请求过大。")
	}
	d := json.NewDecoder(bytes.NewBufferString(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return model.Err("INVALID_REQUEST", "请求字段无效。")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return model.Err("INVALID_REQUEST", "请求包含多余内容。")
	}
	return nil
}
func (s *Service) Dispatch(ctx context.Context, action, payload string) string {
	data, e := s.dispatch(ctx, action, payload)
	res := reply{OK: e == nil, Data: data}
	if e != nil {
		res.Data = nil
		res.Error = model.PublicError(e)
	}
	// Library failures intentionally return a usable cached/partial view with
	// ok=true. Include their nested error in diagnostics without discarding it.
	diagnosticError := res.Error
	if view, ok := data.(model.LibraryView); ok && diagnosticError == nil {
		diagnosticError = view.Error
	}
	if diagnosticError != nil {
		safeAction := action
		if len(safeAction) > 40 {
			safeAction = "unknown"
		}
		switch action {
		case "bootstrap", "settings.save", "account.sendCode", "account.login", "account.restore", "account.logout", "library", "detail", "openLink", "playback.resolve", "playback.cancel", "progress.save", "queue", "bookmarks", "cache.clear", "data.reset", "diagnostics", "comments.list", "comments.thread":
		default:
			safeAction = "unknown"
		}
		s.mu.Lock()
		s.log = append(s.log, diagnosticEvent{Time: model.Now(), Action: safeAction, Code: diagnosticError.Code, HTTPStatus: diagnosticError.HTTPStatus, UpstreamCode: diagnosticError.UpstreamCode})
		if len(s.log) > 100 {
			s.log = s.log[len(s.log)-100:]
		}
		s.mu.Unlock()
	}
	b, e := json.Marshal(res)
	if e != nil {
		return `{"ok":false,"error":{"code":"INTERNAL","message":"响应编码失败。"}}`
	}
	return string(b)
}
func (s *Service) dispatch(ctx context.Context, action, payload string) (any, error) {
	switch action {
	case "comments.list", "comments.thread":
		var v struct {
			Epoch     uint64 `json:"epoch"`
			EpisodeID string `json:"episodeId"`
			CommentID string `json:"commentId"`
			Cursor    string `json:"cursor"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		if (action == "comments.thread" && v.CommentID == "") || (action == "comments.list" && v.CommentID != "") {
			return nil, model.Err("INVALID_REQUEST", "评论请求类型与参数不匹配。")
		}
		return s.Comments(ctx, v.Epoch, v.EpisodeID, v.CommentID, v.Cursor)
	case "account.qrStart":
		if e := s.checkExperimental(); e != nil {
			return nil, e
		}
		q, e := s.session.StartQR(ctx)
		if e != nil {
			return nil, e
		}
		image, e := qrcode.New(q.URL, qrcode.Medium)
		if e != nil {
			s.session.CancelQR(q.ID)
			return nil, model.Err("INTERNAL", "无法生成二维码。")
		}
		return struct {
			ID      string   `json:"id"`
			Modules [][]bool `json:"modules"`
		}{q.ID, image.Bitmap()}, nil
	case "account.qrPoll":
		var v struct {
			ID       string `json:"id"`
			Remember bool   `json:"remember"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		if e := s.checkExperimental(); e != nil {
			return nil, e
		}
		status, e := s.session.PollQR(ctx, v.ID, v.Remember)
		return struct {
			Status string `json:"status"`
		}{status}, e
	case "account.qrCancel":
		var v struct {
			ID string `json:"id"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		s.session.CancelQR(v.ID)
		return nil, nil
	case "bootstrap":
		return s.Bootstrap()
	case "settings.save":
		var v model.Settings
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		return nil, s.SaveSettings(v)
	case "account.sendCode":
		var v struct {
			Phone string `json:"phone"`
			Area  string `json:"area"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		return nil, s.SendCode(ctx, v.Phone, v.Area)
	case "account.login":
		var v struct {
			Epoch    *uint64 `json:"epoch"`
			Phone    string  `json:"phone"`
			Area     string  `json:"area"`
			Code     string  `json:"code"`
			Remember bool    `json:"remember"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		if v.Epoch != nil {
			if e := s.checkExperimental(); e != nil {
				return nil, e
			}
			return nil, s.session.LoginAt(ctx, *v.Epoch, v.Phone, v.Area, v.Code, v.Remember)
		}
		return nil, s.Login(ctx, v.Phone, v.Area, v.Code, v.Remember)
	case "account.cancelLogin":
		var v struct {
			Epoch uint64 `json:"epoch"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		return nil, s.session.CancelLogin(v.Epoch)
	case "account.restore":
		return nil, s.Restore(ctx)
	case "account.logout":
		var v struct {
			Epoch uint64 `json:"epoch"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		return nil, s.LogoutAt(v.Epoch)
	case "library":
		var v struct {
			Epoch     uint64 `json:"epoch"`
			Kind      string `json:"kind"`
			PodcastID string `json:"podcastId"`
			Mode      string `json:"mode"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		return s.Library(ctx, v.Epoch, v.Kind, v.PodcastID, v.Mode)
	case "detail":
		var v struct {
			Epoch uint64 `json:"epoch"`
			Kind  string `json:"kind"`
			ID    string `json:"id"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		return s.Detail(ctx, v.Epoch, v.Kind, v.ID)
	case "openLink":
		var v struct {
			Epoch uint64 `json:"epoch"`
			Text  string `json:"text"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		return s.OpenLink(ctx, v.Epoch, v.Text)
	case "playback.resolve":
		var v struct {
			Epoch      uint64  `json:"epoch"`
			Generation *uint64 `json:"generation"`
			ID         string  `json:"id"`
			RequestID  string  `json:"requestId"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		if v.Generation != nil {
			return s.ResolveAt(ctx, v.Epoch, *v.Generation, v.ID, v.RequestID)
		}
		return s.Resolve(ctx, v.Epoch, v.ID, v.RequestID)
	case "playback.cancel":
		var v struct {
			Epoch      uint64  `json:"epoch"`
			Generation *uint64 `json:"generation"`
			RequestID  string  `json:"requestId"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		if v.Generation != nil {
			return nil, s.CancelResolveAt(v.Epoch, *v.Generation, v.RequestID)
		}
		return nil, s.CancelResolve(v.Epoch, v.RequestID)
	case "progress.save":
		var v struct {
			Epoch    uint64         `json:"epoch"`
			Progress model.Progress `json:"progress"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		return nil, s.SaveProgress(v.Epoch, v.Progress)
	case "queue", "bookmarks":
		var v struct {
			Epoch uint64     `json:"epoch"`
			Op    string     `json:"op"`
			Item  model.Item `json:"item"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		if action == "queue" {
			return s.Queue(v.Epoch, v.Op, v.Item)
		}
		return s.Bookmarks(v.Epoch, v.Op, v.Item)
	case "cache.clear":
		var v struct {
			Epoch uint64 `json:"epoch"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		return nil, s.ClearCache(v.Epoch)
	case "data.reset":
		var v struct {
			Epoch   uint64 `json:"epoch"`
			Confirm string `json:"confirm"`
		}
		if e := decodePayload(payload, &v); e != nil {
			return nil, e
		}
		return nil, s.Reset(v.Epoch, v.Confirm)
	case "diagnostics":
		s.mu.Lock()
		logs := append([]diagnosticEvent{}, s.log...)
		s.mu.Unlock()
		return map[string]any{"version": Version, "platform": runtime.GOOS, "sessionState": s.session.View().State, "events": logs, "note": "仅包含本次进程的业务错误码，不含账号标识、凭据或收听内容。"}, nil
	default:
		return nil, model.Err("UNSUPPORTED", "该业务方法不在允许列表内。")
	}
}
