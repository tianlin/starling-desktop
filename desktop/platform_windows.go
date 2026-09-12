//go:build windows

package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"os"
	"path/filepath"
)

func alreadyRunning(error) bool { return false }
func platformStartupError() string {
	return "桌面启动失败。请检查 WebView2 运行时与应用资源。"
}
func platformWebview(dataDir string) (string, func()) {
	path := filepath.Join(dataDir, "webview-cache")
	_ = os.RemoveAll(path)
	return path, func() { _ = os.RemoveAll(path) }
}
func configurePlatform(opts *options.App, a *App, webview string) {
	opts.Windows = &windows.Options{Theme: windows.Light, WebviewUserDataPath: webview, DisablePinchZoom: true, DLLSearchPaths: windows.DLLSearchSystem32, OnSuspend: func() {
		a.mu.Lock()
		ctx := a.ctx
		a.mu.Unlock()
		if ctx != nil {
			wruntime.EventsEmit(ctx, "desktop:pause")
		}
	}, OnResume: func() {
		a.mu.Lock()
		ctx := a.ctx
		a.mu.Unlock()
		if ctx != nil {
			wruntime.EventsEmit(ctx, "desktop:pause")
		}
	}}
}
