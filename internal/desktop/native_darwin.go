//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -mmacosx-version-min=14.0
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void starlingStart(const char *name);
void starlingStop(void);
void starlingNotify(const char *name);
int starlingOpen(const char *url);
void starlingAlert(const char *text);
*/
import "C"
import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"starling/internal/security"
	"sync"
	"syscall"
	"unsafe"
)

func init() { runtime.LockOSThread() }

var ErrAlreadyRunning = errors.New("Starling is already running")
var macState struct {
	sync.Mutex
	integration  *Integration
	notification string
}

type Integration struct {
	hooks Hooks
	once  sync.Once
}

func Start(hooks Hooks) *Integration {
	n := &Integration{hooks: hooks}
	macState.Lock()
	macState.integration = n
	name := macState.notification
	macState.Unlock()
	c := C.CString(name)
	C.starlingStart(c)
	C.free(unsafe.Pointer(c))
	return n
}
func (*Integration) Info() Info { return Info{Platform: "darwin", Tray: true, MediaKey: false} }
func (n *Integration) Close() {
	n.once.Do(func() { macState.Lock(); macState.integration = nil; macState.Unlock(); C.starlingStop() })
}

//export starlingEvent
func starlingEvent(event C.int) {
	macState.Lock()
	n := macState.integration
	macState.Unlock()
	if n == nil {
		return
	}
	var f func()
	switch event {
	case 1:
		f = n.hooks.Show
	case 2:
		f = n.hooks.Toggle
	case 3:
		f = n.hooks.Quit
	case 4:
		f = n.hooks.Pause
	}
	// Cocoa callbacks must return before Wails queues further main-thread work.
	if f != nil {
		go f()
	}
}
func AcquireInstance(dataDir string) (func(), error) {
	if e := os.MkdirAll(dataDir, 0700); e != nil {
		return nil, e
	}
	absolute, e := filepath.Abs(dataDir)
	if e != nil {
		return nil, e
	}
	sum := sha256.Sum256([]byte(absolute))
	name := fmt.Sprintf("io.github.tianlin.starling.show.%d.%x", os.Getuid(), sum[:16])
	f, e := os.OpenFile(filepath.Join(dataDir, "instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		if errors.Is(e, syscall.EWOULDBLOCK) || errors.Is(e, syscall.EAGAIN) {
			c := C.CString(name)
			C.starlingNotify(c)
			C.free(unsafe.Pointer(c))
			return nil, ErrAlreadyRunning
		}
		return nil, e
	}
	macState.Lock()
	macState.notification = name
	macState.Unlock()
	var once sync.Once
	return func() { once.Do(func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }) }, nil
}
func OpenURL(url string) error {
	if e := security.ValidateExternalURL(url); e != nil {
		return e
	}
	c := C.CString(url)
	defer C.free(unsafe.Pointer(c))
	if C.starlingOpen(c) == 0 {
		return errors.New("default browser could not open URL")
	}
	return nil
}
func Alert(text string) {
	fmt.Fprintln(os.Stderr, text)
	c := C.CString(text)
	defer C.free(unsafe.Pointer(c))
	C.starlingAlert(c)
}
