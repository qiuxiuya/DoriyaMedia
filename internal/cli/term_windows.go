//go:build windows

package cli

import (
	"os"
	"syscall"
	"unsafe"
)

// Windows console mode. The file is not a console for a pipe or a redirected
// file, in which case echo is left alone and the caller reads a plain line.
const enableEchoInput = 0x0004

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

func getConsoleMode(handle syscall.Handle) (uint32, error) {
	var mode uint32
	r, _, err := procGetConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return 0, err
	}
	return mode, nil
}

func setConsoleMode(handle syscall.Handle, mode uint32) error {
	r, _, err := procSetConsoleMode.Call(uintptr(handle), uintptr(mode))
	if r == 0 {
		return err
	}
	return nil
}

func disableEcho(f *os.File) (*uint32, bool) {
	handle := syscall.Handle(f.Fd())
	mode, err := getConsoleMode(handle)
	if err != nil {
		return nil, false
	}
	old := mode
	if err := setConsoleMode(handle, mode&^enableEchoInput); err != nil {
		return nil, false
	}
	return &old, true
}

func restoreEcho(f *os.File, state *uint32) {
	if state == nil {
		return
	}
	_ = setConsoleMode(syscall.Handle(f.Fd()), *state)
}
