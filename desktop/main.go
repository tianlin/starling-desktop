//go:build windows || darwin

package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"starling/internal/app"
	native "starling/internal/desktop"
	"starling/internal/model"
	"starling/internal/provider"
	"starling/internal/security"
	"starling/internal/store"
	"strings"
	"sync"
	"time"
)

//go:embed assets/*
var assets embed.FS

type App struct {
	mu          sync.Mutex
	ctx         context.Context
	service     *app.Service
	integration *native.Integration
	gate        native.QuitGate
	dialog      bool
	ready       bool
}

func (a *App) startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
	integration := native.Start(native.Hooks{
		Show:   func() { wruntime.WindowShow(ctx); wruntime.WindowUnminimise(ctx) },
		Toggle: func() { wruntime.EventsEmit(ctx, "desktop:toggle") },
		Pause:  func() { wruntime.EventsEmit(ctx, "desktop:pause") },
		Quit:   func() { a.requestQuit() },
	})
	a.mu.Lock()
	a.integration = integration
	a.mu.Unlock()
}
func (a *App) info() native.Info {
	a.mu.Lock()
	n := a.integration
	a.mu.Unlock()
	if n == nil {
		return native.Info{Platform: runtime.GOOS, Message: "原生集成正在初始化。"}
	}
	info := n.Info()
	info.Platform = runtime.GOOS
	return info
}
func (a *App) requestQuit() {
	if !a.gate.Request() {
		return
	}
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	wruntime.EventsEmit(ctx, "desktop:before-quit")
	// An unresponsive renderer cannot keep the process alive indefinitely.
	// Recovery still uses the last successful five-second persistence checkpoint.
	go func() {
		time.Sleep(6 * time.Second)
		if !a.gate.Allowed() {
			fmt.Fprintln(os.Stderr, "Starling quit fallback")
			a.gate.Allow()
			wruntime.Quit(ctx)
		}
	}()
}
func (a *App) beforeClose(ctx context.Context) bool {
	if a.gate.Allowed() {
		return false
	}
	if a.gate.Requested() {
		return true
	}
	if runtime.GOOS == "darwin" {
		a.requestQuit()
		return true
	}
	behavior := a.service.Settings().CloseBehavior
	if behavior == "ask" {
		a.mu.Lock()
		if a.dialog {
			a.mu.Unlock()
			return true
		}
		a.dialog = true
		a.mu.Unlock()
		answer, e := wruntime.MessageDialog(ctx, wruntime.MessageDialogOptions{Type: wruntime.QuestionDialog, Title: "关闭 Starling", Message: "最小化到托盘将继续播放；退出会停止音频并保存本机进度。", Buttons: []string{"最小化到托盘", "退出", "取消"}, DefaultButton: "取消", CancelButton: "取消"})
		a.mu.Lock()
		a.dialog = false
		a.mu.Unlock()
		if e != nil || answer == "取消" || answer == "" {
			return true
		}
		if answer == "最小化到托盘" {
			behavior = "tray"
		} else {
			behavior = "exit"
		}
	}
	if behavior == "tray" {
		if !a.info().Tray {
			_, _ = wruntime.MessageDialog(ctx, wruntime.MessageDialogOptions{Type: wruntime.WarningDialog, Title: "托盘不可用", Message: "为防止窗口隐藏后无法找回，本次不隐藏。请在设置中选择退出。"})
			return true
		}
		wruntime.WindowHide(ctx)
		return true
	}
	a.requestQuit()
	return true
}

// Call is the only bound method. It never accepts filesystem paths, arbitrary HTTP targets or code.
func (a *App) Call(action, payload string) string {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return fail("NOT_READY", "桌面后端尚未就绪。")
	}
	if len(payload) > 1<<20 {
		return fail("INVALID_REQUEST", "请求过大。")
	}
	switch action {
	case "desktop.ready":
		a.mu.Lock()
		if !a.ready {
			fmt.Fprintln(os.Stderr, "Starling application ready")
			a.ready = true
		}
		a.mu.Unlock()
		return ok(nil)
	case "desktop.info":
		return ok(a.info())
	case "desktop.quitReady":
		if !a.gate.Requested() {
			return fail("INVALID_STATE", "未请求退出。")
		}
		a.gate.Allow()
		fmt.Fprintln(os.Stderr, "Starling progress flushed")
		go wruntime.Quit(ctx)
		return ok(nil)
	case "desktop.openExternal":
		var v struct {
			URL string `json:"url"`
		}
		d := json.NewDecoder(strings.NewReader(payload))
		d.DisallowUnknownFields()
		if d.Decode(&v) != nil {
			return fail("INVALID_REQUEST", "链接参数无效。")
		}
		if e := native.OpenURL(v.URL); e != nil {
			return fail("INVALID_URL", "链接不受支持或无法打开默认浏览器。")
		}
		return ok(nil)
	case "desktop.exportDiagnostics":
		filename, e := wruntime.SaveFileDialog(ctx, wruntime.SaveDialogOptions{Title: "保存脱敏诊断", DefaultFilename: "starling-diagnostics.json", Filters: []wruntime.FileFilter{{DisplayName: "JSON 文件", Pattern: "*.json"}}})
		if e != nil {
			return fail("DISK", "文件对话框未完成。")
		}
		if filename == "" {
			return ok(nil)
		}
		raw := a.service.Dispatch(ctx, "diagnostics", `{}`)
		var v struct {
			OK   bool            `json:"ok"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal([]byte(raw), &v) != nil || !v.OK {
			return fail("INTERNAL", "诊断生成失败。")
		}
		if e = os.WriteFile(filename, v.Data, 0600); e != nil {
			return fail("DISK", "无法保存诊断文件。")
		}
		return ok(nil)
	default:
		return a.service.Dispatch(ctx, action, payload)
	}
}
func ok(value any) string {
	b, _ := json.Marshal(map[string]any{"ok": true, "data": value})
	return string(b)
}
func fail(code, message string) string {
	b, _ := json.Marshal(map[string]any{"ok": false, "error": model.AppError{Code: code, Message: message}})
	return string(b)
}
func middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' https://image.xyzcdn.net https://bts-image.xyzcdn.net https://media.xyzcdn.net; media-src https://media.xyzcdn.net https://media.xiaoyuzhoufm.com; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-src 'none'; form-action 'none'")
		next.ServeHTTP(w, r)
	})
}
func main() {
	base, e := os.UserConfigDir()
	if e != nil {
		native.Alert("无法取得当前用户的数据目录。")
		return
	}
	dataDir := filepath.Join(base, "Starling")
	release, e := native.AcquireInstance(dataDir)
	if e != nil {
		if !alreadyRunning(e) {
			native.Alert(e.Error())
		}
		return
	}
	defer release()
	if e = os.MkdirAll(dataDir, 0700); e != nil {
		native.Alert("无法创建用户数据目录。")
		return
	}
	db, e := store.Open(filepath.Join(dataDir, "metadata.db"))
	if e != nil {
		native.Alert("本机数据库不可用，原有数据未被覆盖。请检查文件权限或恢复备份。")
		return
	}
	defer db.Close()
	service := app.New(provider.New(), db, security.NewVault(filepath.Join(dataDir, "session.vault")))
	defer service.Close()
	webview, cleanup := platformWebview(dataDir)
	defer cleanup()
	ui, e := fs.Sub(assets, "assets")
	if e != nil {
		native.Alert("前端资源不可用。")
		return
	}
	a := &App{service: service}
	opts := &options.App{
		Title: "Starling · 星听 " + app.Version + "（非官方）", Width: 1240, Height: 840, MinWidth: 900, MinHeight: 650,
		AssetServer: &assetserver.Options{Assets: ui, Middleware: middleware}, Bind: []interface{}{a},
		OnStartup: a.startup, OnBeforeClose: a.beforeClose,
		OnDomReady: func(context.Context) { fmt.Fprintln(os.Stderr, "Starling desktop ready") },
		OnShutdown: func(ctx context.Context) {
			a.mu.Lock()
			n := a.integration
			a.mu.Unlock()
			if n != nil {
				n.Close()
			}
		},
	}
	configurePlatform(opts, a, webview)
	e = wails.Run(opts)
	if e != nil {
		native.Alert(fmt.Sprintf("%s\n%T", platformStartupError(), e))
	}
}
