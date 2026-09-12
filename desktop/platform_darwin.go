//go:build darwin

package main

import (
	"errors"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	native "starling/internal/desktop"
)

func alreadyRunning(e error) bool { return errors.Is(e, native.ErrAlreadyRunning) }
func platformStartupError() string {
	return "桌面启动失败。请检查 macOS 版本与应用资源。"
}
func platformWebview(string) (string, func()) { return "", func() {} }
func configurePlatform(opts *options.App, a *App, _ string) {
	// Wails handles the red close button by hiding NSApp without sending its quit
	// callback. Cmd-Q and system Quit still reach OnBeforeClose and our flush gate.
	opts.HideWindowOnClose = true
	opts.Mac = &mac.Options{Appearance: mac.NSAppearanceNameAqua, DisableZoom: true, Preferences: &mac.Preferences{TabFocusesLinks: mac.Enabled}}
	appMenu := menu.NewMenu()
	starling := appMenu.AddSubmenu("Starling")
	starling.AddText("退出 Starling", keys.CmdOrCtrl("q"), func(*menu.CallbackData) { a.requestQuit() })
	appMenu.Append(menu.EditMenu())
	opts.Menu = appMenu
}
