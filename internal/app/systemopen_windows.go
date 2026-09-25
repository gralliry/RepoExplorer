//go:build windows

package app

import (
	"fmt"
	"syscall"
	"unsafe"
)

const swShownormal = 1

var (
	shell32DLL       = syscall.NewLazyDLL("shell32.dll")
	procShellExecute = shell32DLL.NewProc("ShellExecuteW")
)

// openWithSystemDefault hands the file to Windows exactly like double-clicking
// it in Explorer: the shell looks up the associated program and launches it.
// Calling the API directly avoids spawning cmd.exe (which would flash a console
// window) and avoids any shell-quoting problems with spaces in paths.
func openWithSystemDefault(path string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}

	ret, _, _ := procShellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		0,
		0,
		uintptr(swShownormal),
	)
	// ShellExecute returns a value > 32 on success.
	if ret <= 32 {
		return fmt.Errorf("系统打不开这个文件（错误码 %d），可能没有关联的程序", ret)
	}
	return nil
}
