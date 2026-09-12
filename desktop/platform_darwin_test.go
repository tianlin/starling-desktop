//go:build darwin

package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	"testing"
)

func TestDarwinCloseAndMenuOptions(t *testing.T) {
	o := &options.App{}
	configurePlatform(o, &App{}, "")
	if !o.HideWindowOnClose || o.Mac == nil || o.Menu == nil || o.Windows != nil {
		t.Fatal("incorrect macOS lifecycle options")
	}
	q := o.Menu.Items[0].SubMenu.Items[0]
	if q.Accelerator == nil || q.Accelerator.Key != "q" || q.Click == nil {
		t.Fatal("missing explicit quit handshake")
	}
}
