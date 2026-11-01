//go:build !windows

package cli

import (
	"os"
	"syscall"
	"unsafe"
)

// disableEcho turns off terminal echo so a typed password is not printed. It
// reports false when the file is not a terminal (a pipe, a script), in which
// case the caller reads a plain line.
func disableEcho(f *os.File) (*syscall.Termios, bool) {
	fd := int(f.Fd())
	old, err := getTermios(fd)
	if err != nil {
		return nil, false
	}
	next := *old
	next.Lflag &^= syscall.ECHO
	next.Lflag |= syscall.ICANON | syscall.ISIG
	if err := setTermios(fd, &next); err != nil {
		return nil, false
	}
	return old, true
}

func restoreEcho(f *os.File, state *syscall.Termios) {
	if state == nil {
		return
	}
	_ = setTermios(int(f.Fd()), state)
}

func getTermios(fd int) (*syscall.Termios, error) {
	var t syscall.Termios
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd),
		uintptr(ioctlGetTermios), uintptr(unsafe.Pointer(&t)), 0, 0, 0)
	if errno != 0 {
		return nil, errno
	}
	return &t, nil
}

func setTermios(fd int, t *syscall.Termios) error {
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd),
		uintptr(ioctlSetTermios), uintptr(unsafe.Pointer(t)), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
