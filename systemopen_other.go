//go:build !windows

package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openWithSystemDefault asks the desktop environment to open the file with its
// associated application.
func openWithSystemDefault(path string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	if err := exec.Command(opener, path).Start(); err != nil {
		return fmt.Errorf("无法打开文件：%w", err)
	}
	return nil
}
