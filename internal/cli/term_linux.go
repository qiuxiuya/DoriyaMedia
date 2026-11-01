//go:build linux

package cli

import "syscall"

// TCGETS / TCSETS on Linux. Kept in a build-tagged file because the constants
// differ between Linux, the BSDs and macOS.
const (
	ioctlGetTermios = syscall.TCGETS
	ioctlSetTermios = syscall.TCSETS
)
