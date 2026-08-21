package main

import (
	"os/exec"
	"syscall"
)

// hideWindow 设置 SysProcAttr，使通过 os/exec 创建的进程不弹出控制台窗口。
// HideWindow 对控制台子进程不够可靠，故同时加 CREATE_NO_WINDOW(0x08000000)
// 彻底禁止创建控制台窗口，避免 PowerShell 后台运行时闪现黑框干扰用户。
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
}
