//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	swShowNormal      = 1
	seErrAccessDenied = 5 // ShellExecuteW 返回值 ≤32 表示失败；5 通常对应 UAC 被用户拒绝/取消
)

var shell32 = windows.NewLazySystemDLL("shell32.dll")
var procShellExecuteW = shell32.NewProc("ShellExecuteW")

// ahkPaths 解析随包分发的 AHK 解释器与主脚本路径（相对 exe 目录，与 vrtx.json 同款定位方式）
func ahkPaths() (interpreter, script string) {
	exe, err := os.Executable()
	if err != nil {
		return "", ""
	}
	dir := filepath.Join(filepath.Dir(exe), "ahk")
	return filepath.Join(dir, "AutoHotkey64.exe"), filepath.Join(dir, "WindowJump.ahk")
}

// launchAHK 以管理员权限拉起 AutoHotkey 运行 WindowJump.ahk（触发标准 UAC 授权框）。
//
// 提权后的子进程不受普通权限的 vrtx 控制，故把主程序 PID 作为首参数传给
// VrtxWatchdog.ahk 看门狗：脚本侧每秒轮询父进程存活，vrtx 退出后自动跟随退出。
//
// 用户拒绝 UAC 授权或 AHK 组件缺失时返回错误，调用方优雅跳过、主流程不受影响。
func launchAHK() error {
	interp, script := ahkPaths()
	if interp == "" {
		return fmt.Errorf("无法定位自身可执行文件")
	}
	for _, p := range []string{interp, script} {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("缺少 AHK 组件: %s", p)
		}
	}
	args := fmt.Sprintf(`"%s" %d`, script, os.Getpid())
	workDir := filepath.Dir(script)

	verbPtr, _ := windows.UTF16PtrFromString("runas")
	interpPtr, _ := windows.UTF16PtrFromString(interp)
	argsPtr, _ := windows.UTF16PtrFromString(args)
	dirPtr, _ := windows.UTF16PtrFromString(workDir)
	ret, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(interpPtr)),
		uintptr(unsafe.Pointer(argsPtr)),
		uintptr(unsafe.Pointer(dirPtr)),
		swShowNormal,
	)
	if ret <= 32 {
		if ret == seErrAccessDenied {
			return fmt.Errorf("用户拒绝了 UAC 授权")
		}
		return fmt.Errorf("ShellExecute 失败 (code=%d)", ret)
	}
	return nil
}
