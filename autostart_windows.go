//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/lutischan-ferenc/systray"
)

// taskName 是任务计划程序中 VRTX 任务的文件夹名称
const taskFolder = `\VRTX`
const taskName = `VRTX`

// autostartFileExists 快速判断自启任务是否存在（仅供菜单初始视觉使用）
func autostartFileExists() bool {
	cmd := exec.Command("schtasks", "/query", "/TN", taskFolder+"\\"+taskName, "/FO", "LIST", "/V")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	return err == nil && strings.Contains(string(out), taskName)
}

// toggleAutoStart 托盘菜单开关：决策基于任务计划程序中的任务状态
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
	logInfo("已开启开机自启动")
}

// autostartState 描述开机自启动的状态
type autostartState int

const (
	autostartOff autostartState = iota // 任务不存在，未开启
	autostartOn                        // 任务存在
)

// detectAutoStart 检测 VRTX 自启动任务是否存在
func detectAutoStart() autostartState {
	cmd := exec.Command("schtasks", "/query", "/TN", taskFolder+"\\"+taskName, "/FO", "LIST", "/V")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return autostartOff
	}
	if strings.Contains(string(out), taskName) {
		return autostartOn
	}
	return autostartOff
}

// enableAutoStart 创建/覆盖自启动任务，使用任务计划程序以最高权限运行
func enableAutoStart() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位自身可执行文件失败: %w", err)
	}
	cmd := exec.Command("schtasks", "/create", "/TN", taskFolder+"\\"+taskName, "/TR", exe, "/SC", "ONLOGON", "/RL", "HIGHEST", "/DELAY", "0000:03", "/F")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("创建自启动任务失败: %v\n%s", err, string(out))
	}
	return nil
}

// disableAutoStart 删除自启动任务（不存在视为成功）
func disableAutoStart() error {
	cmd := exec.Command("schtasks", "/delete", "/TN", taskFolder+"\\"+taskName, "/F")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("删除自启动任务失败: %v\n%s", err, string(out))
	}
	return nil
}
