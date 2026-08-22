//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lutischan-ferenc/systray"
)

// autostartFileExists 快速判断自启动 lnk 是否存在（不验证指向，
// 仅供菜单初始视觉使用；真实状态以 detectAutoStart 为准）
func autostartFileExists() bool {
	_, err := os.Stat(startupLnkPath())
	return err == nil
}

// toggleAutoStart 托盘菜单开关：决策永远基于文件系统当前的真实三态，
// 对勾视觉只是缓存——即使外部手动动过 lnk 也不会误操作。
// Stale 态单击视为修复（覆盖重建为当前 exe）。
func toggleAutoStart(item *systray.MenuItem) {
	state := detectAutoStart()
	if state == autostartOn {
		if err := disableAutoStart(); err != nil {
			logError("关闭开机自启动失败：%v", err)
			return
		}
		item.Uncheck()
		logInfo("已关闭开机自启动")
		return
	}
	if err := enableAutoStart(); err != nil {
		logError("开启开机自启动失败：%v", err)
		return
	}
	item.Check()
	if state == autostartStale {
		logInfo("检测到指向旧路径的自启动项，已修复为当前程序")
	} else {
		logInfo("已开启开机自启动")
	}
}

// autostartState 描述开机自启动的三种真实状态
type autostartState int

const (
	autostartOff   autostartState = iota // 无 lnk，未开启
	autostartOn                          // lnk 存在且指向当前 exe
	autostartStale                       // lnk 存在但指向别的路径（exe 改名/移动后的残留）
)

// startupLnkPath 返回当前用户 Startup 目录下的自启动快捷方式路径
func startupLnkPath() string {
	appData := os.Getenv("APPDATA")
	return filepath.Join(appData, "Microsoft", "Windows",
		"Start Menu", "Programs", "Startup", "VRTX.lnk")
}

// detectAutoStart 三态判定：
//
//	不存在 → Off；存在且指向当前 exe → On；存在但指向别处或无法验证 → Stale。
//	"已开启"必须同时满足存在且指向自己；一切异常态归入 Stale，单击即覆盖修复。
func detectAutoStart() autostartState {
	lnk := startupLnkPath()
	if _, err := os.Stat(lnk); err != nil {
		return autostartOff
	}
	if lnkTargetMatchesExe(lnk) {
		return autostartOn
	}
	return autostartStale
}

// lnkTargetMatchesExe 判定 lnk 的目标是否就是当前运行的 exe。
// 比较全程在 PowerShell 内部完成——Get-Item 归一化 8.3 短路径与大小写，
// 跨进程边界只传递 'SAME'/'DIFF' 纯 ASCII 判定词，
// 规避 PS 控制台编码（GBK/UTF-8）与路径形态差异导致的误判。
func lnkTargetMatchesExe(lnk string) bool {
	exe, err := os.Executable()
	if err != nil {
		logDebug("定位自身可执行文件失败：%v", err)
		return false
	}
	script := fmt.Sprintf(`
$ws = New-Object -ComObject WScript.Shell
$t = $ws.CreateShortcut(%s).TargetPath
try {
  $a = Get-Item -LiteralPath $t -ErrorAction Stop
  $b = Get-Item -LiteralPath %s -ErrorAction Stop
  if ($a.FullName -ieq $b.FullName) { 'SAME' } else { 'DIFF' }
} catch { 'DIFF' }
`, psSingleQuote(lnk), psSingleQuote(exe))
	out, err := powershellOut(script)
	if err != nil {
		logDebug("读取自启动快捷方式目标失败：%v", err)
		return false
	}
	return out == "SAME"
}

// enableAutoStart 创建/覆盖自启动快捷方式，指向当前运行的 exe。
// 幂等操作：对失效残留项调用即等于修复。产物经校验确保成功语义。
func enableAutoStart() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位自身可执行文件失败: %w", err)
	}
	lnk := startupLnkPath()
	script := fmt.Sprintf(`
$ws = New-Object -ComObject WScript.Shell
$s = $ws.CreateShortcut(%s)
$s.TargetPath = %s
$s.WorkingDirectory = %s
$s.Save()
`, psSingleQuote(lnk), psSingleQuote(exe), psSingleQuote(filepath.Dir(exe)))
	runPowershell(script)
	if _, err := os.Stat(lnk); err != nil {
		return fmt.Errorf("自启动快捷方式创建失败: %s", lnk)
	}
	return nil
}

// disableAutoStart 删除自启动快捷方式（不存在视为成功）
func disableAutoStart() error {
	err := os.Remove(startupLnkPath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
