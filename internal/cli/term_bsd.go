//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package cli

import "syscall"

// TIOCGETA / TIOCSETA on macOS and the BSDs.
const (
	ioctlGetTermios = syscall.TIOCGETA
	ioctlSetTermios = syscall.TIOCSETA
)
