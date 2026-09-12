//go:build darwin && cgo

package desktop

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDarwinInstanceContentionAndRecovery(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "中文数据")
	release, e := AcquireInstance(dir)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = AcquireInstance(dir); !errors.Is(e, ErrAlreadyRunning) {
		release()
		t.Fatal("second instance acquired database", e)
	}
	release()
	release()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	child := exec.Command(exe, "-test.run=^TestDarwinCrashLockHelper$")
	child.Env = append(os.Environ(), "STARLING_TEST_LOCK_DIR="+dir)
	if b, e := child.CombinedOutput(); e != nil {
		t.Fatalf("child: %v %s", e, b)
	}
	release, e = AcquireInstance(dir)
	if e != nil {
		t.Fatal("crashed process retained lock", e)
	}
	release()
}
func TestDarwinCrashLockHelper(t *testing.T) {
	dir := os.Getenv("STARLING_TEST_LOCK_DIR")
	if dir == "" {
		return
	}
	if _, e := AcquireInstance(dir); e != nil {
		t.Fatal(e)
	}
	os.Exit(0) // Deliberately skip release to exercise kernel cleanup.
}
