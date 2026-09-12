//go:build linux || darwin

package store

import "os/exec"

func hideTestChild(cmd *exec.Cmd) {}
