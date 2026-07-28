package main

import (
	"os/exec"
	"syscall"
)

// hideWindow 设置 SysProcAttr.HideWindow 标志，使通过 os/exec 创建的进程不弹出控制台窗口。
// 避免 PowerShell 后台运行时闪现黑框干扰用户。
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
