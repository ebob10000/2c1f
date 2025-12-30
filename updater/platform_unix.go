//go:build !windows

package updater

import (
	"syscall"
)

func getSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true, // Create new session (detach from parent)
	}
}
