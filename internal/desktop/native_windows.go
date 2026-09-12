//go:build windows

package desktop

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"starling/internal/security"
	"starling/internal/winapi"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var user32 = winapi.SystemDLL("user32.dll")
var shell32 = winapi.SystemDLL("shell32.dll")
var kernel32 = winapi.SystemDLL("kernel32.dll")
var defWindowProc = user32.NewProc("DefWindowProcW")
var destroyWindow = user32.NewProc("DestroyWindow")
var postMessage = user32.NewProc("PostMessageW")
var notifyIcon = shell32.NewProc("Shell_NotifyIconW")
var unregisterHotKey = user32.NewProc("UnregisterHotKey")

const trayMessage = 0x8001

type point struct{ X, Y int32 }
type message struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}
type windowClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}
type guid struct {
	A    uint32
	B, C uint16
	D    [8]byte
}
type notifyData struct {
	Size                uint32
	Hwnd                uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                guid
	BalloonIcon         uintptr
}
type Integration struct {
	mu      sync.Mutex
	hwnd    uintptr
	info    Info
	hooks   Hooks
	done    chan struct{}
	ready   chan struct{}
	data    notifyData
	taskbar uint32
}

func Start(hooks Hooks) *Integration {
	n := &Integration{hooks: hooks, done: make(chan struct{}), ready: make(chan struct{})}
	go n.run()
	<-n.ready
	return n
}
func (n *Integration) Info() Info { n.mu.Lock(); defer n.mu.Unlock(); return n.info }
func (n *Integration) Close() {
	n.mu.Lock()
	h := n.hwnd
	n.mu.Unlock()
	if h != 0 {
		postMessage.Call(h, 0x0010, 0, 0)
	}
	select {
	case <-n.done:
	case <-time.After(2 * time.Second):
	}
}
func invoke(f func()) {
	if f != nil {
		go f()
	}
}
func (n *Integration) addTray() {
	result, _, _ := notifyIcon.Call(0, uintptr(unsafe.Pointer(&n.data)))
	n.mu.Lock()
	n.info.Tray = result != 0
	if result == 0 {
		n.info.Message = "托盘创建失败，窗口不会自动隐藏。"
	}
	n.mu.Unlock()
}
func (n *Integration) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(n.done)
	module, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	class, _ := syscall.UTF16PtrFromString("Starling.Native.Helper")
	cb := syscall.NewCallback(func(hwnd uintptr, msg uint32, w, l uintptr) uintptr {
		if n.taskbar != 0 && msg == n.taskbar {
			n.addTray()
			return 0
		}
		switch msg {
		case trayMessage:
			switch uint32(l) {
			case 0x0202:
				invoke(n.hooks.Show)
			case 0x0205:
				n.popup(hwnd)
			}
			return 0
		case 0x0312:
			if w == 1 {
				invoke(n.hooks.Toggle)
			}
			return 0
		case 0x0218:
			if w == 4 || w == 7 || w == 0x12 {
				invoke(n.hooks.Pause)
			}
			return 1
		case 0x0219:
			if w == 0x8004 {
				invoke(n.hooks.Pause)
			} // conservative pause after device removal
		case 0x0010:
			destroyWindow.Call(hwnd)
			return 0
		case 0x0002:
			unregisterHotKey.Call(hwnd, 1)
			notifyIcon.Call(2, uintptr(unsafe.Pointer(&n.data)))
			user32.NewProc("PostQuitMessage").Call(0)
			return 0
		}
		result, _, _ := defWindowProc.Call(hwnd, uintptr(msg), w, l)
		return result
	})
	wc := windowClass{Proc: cb, Instance: module, ClassName: class}
	wc.Size = uint32(unsafe.Sizeof(wc))
	atom, _, _ := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		n.mu.Lock()
		n.info.Message = "原生窗口注册失败。"
		n.mu.Unlock()
		close(n.ready)
		return
	}
	defer func() {
		user32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(class)), module)
		runtime.KeepAlive(class)
	}()
	title, _ := syscall.UTF16PtrFromString("Starling Desktop Helper")
	hwnd, _, _ := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)), 0, 0, 0, 0, 0, 0, 0, module, 0)
	if hwnd == 0 {
		n.mu.Lock()
		n.info.Message = "原生辅助窗口创建失败。"
		n.mu.Unlock()
		close(n.ready)
		return
	}
	n.mu.Lock()
	n.hwnd = hwnd
	n.mu.Unlock()
	icon, _, _ := user32.NewProc("LoadIconW").Call(0, 32512) // system fallback; no external file loaded
	n.data = notifyData{Hwnd: hwnd, ID: 1, Flags: 1 | 2 | 4, Callback: trayMessage, Icon: icon}
	n.data.Size = uint32(unsafe.Sizeof(n.data))
	tip, _ := syscall.UTF16FromString("Starling · 星听")
	copy(n.data.Tip[:], tip)
	n.addTray()
	hotkey, _, _ := user32.NewProc("RegisterHotKey").Call(hwnd, 1, 0x4000, 0xB3)
	n.mu.Lock()
	n.info.MediaKey = hotkey != 0
	if hotkey == 0 {
		n.info.Message += " 媒体键注册失败或被其他程序占用。"
	}
	n.mu.Unlock()
	taskbar, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	id, _, _ := user32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(taskbar)))
	n.taskbar = uint32(id)
	close(n.ready)
	var msg message
	for {
		result, _, _ := user32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if result == 0 || int32(result) == -1 {
			break
		}
		user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&msg)))
		user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&msg)))
	}
	n.mu.Lock()
	n.hwnd = 0
	n.info.Tray = false
	n.info.MediaKey = false
	n.mu.Unlock()
}
func (n *Integration) popup(hwnd uintptr) {
	menu, _, _ := user32.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		return
	}
	defer user32.NewProc("DestroyMenu").Call(menu)
	for i, label := range []string{"显示 Starling", "播放 / 暂停", "退出"} {
		text, _ := syscall.UTF16PtrFromString(label)
		user32.NewProc("AppendMenuW").Call(menu, 0, uintptr(i+1), uintptr(unsafe.Pointer(text)))
	}
	var p point
	user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&p)))
	user32.NewProc("SetForegroundWindow").Call(hwnd)
	selected, _, _ := user32.NewProc("TrackPopupMenu").Call(menu, 0x100|0x80, uintptr(p.X), uintptr(p.Y), 0, hwnd, 0)
	switch selected {
	case 1:
		invoke(n.hooks.Show)
	case 2:
		invoke(n.hooks.Toggle)
	case 3:
		invoke(n.hooks.Quit)
	}
	postMessage.Call(hwnd, 0, 0, 0)
}
func AcquireInstance(dataDir string) (func(), error) {
	sum := sha256.Sum256([]byte(dataDir))
	name, _ := syscall.UTF16PtrFromString(fmt.Sprintf("Local\\Starling.Desktop.%x", sum[:12]))
	handle, _, e := kernel32.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, fmt.Errorf("无法创建单实例锁")
	}
	release := func() { kernel32.NewProc("CloseHandle").Call(handle) }
	if e == syscall.Errno(183) {
		release()
		return nil, fmt.Errorf("Starling 已经运行，请从托盘打开")
	}
	return release, nil
}
func OpenURL(url string) error {
	if e := security.ValidateExternalURL(url); e != nil {
		return e
	}
	operation, _ := syscall.UTF16PtrFromString("open")
	target, e := syscall.UTF16PtrFromString(url)
	if e != nil {
		return e
	}
	result, _, _ := shell32.NewProc("ShellExecuteW").Call(0, uintptr(unsafe.Pointer(operation)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
	if result <= 32 {
		return fmt.Errorf("无法打开默认浏览器")
	}
	return nil
}
func Alert(message string) {
	text, _ := syscall.UTF16PtrFromString(message)
	title, _ := syscall.UTF16PtrFromString("Starling · 星听")
	user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}
