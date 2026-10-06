//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/lutischan-ferenc/systray"
)

// taskName 是任务计划程序中 VRTX 任务的名称
const taskName = "VRTX"

// autostartState 描述开机自启动的状态
type autostartState int

const (
	autostartOff autostartState = iota // 任务不存在，未开启
	autostartOn                        // 任务存在
)

// detectAutoStart 判断自启动任务是否已创建。
//
// 只看 schtasks 的退出码，不解析它的输出，也不用输出里的任务名做判断：
// 该输出受系统语言与代码页影响（中文系统上字段名显示为“任务名”），
// 而退出码是唯一语言无关、也不会因编码而误判的信号。
func detectAutoStart() autostartState {
	cmd := exec.Command("schtasks", "/query", "/TN", taskName)
	hideWindow(cmd)
	if cmd.Run() == nil {
		return autostartOn
	}
	return autostartOff
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

// repairAutoStartTask 在任务已存在时，把它重写为指向当前可执行文件。
//
// 程序升级或更换安装位置后，旧任务会继续指向已经不存在的路径：开机时什么都
// 不会发生，而托盘菜单仍显示“已勾选”，用户无从察觉。相比解析任务内容去比对
// 路径（schtasks 的输出受系统语言与代码页影响，比对并不可靠），直接按当前
// 路径重写一遍更简单也更准确；任务不存在时这里直接返回，不会凭空替用户开启
// 自启动。
//
// 代价是重写会连同触发器、运行等级等一起恢复成程序内置的那一套：用户若手工
// 改过这个任务（例如换触发器、把它停用），下次启动就会被改回来。这里假定
// VRTX 任务由本程序独占，因此没有为这种情况加检测。
func repairAutoStartTask() {
	if detectAutoStart() != autostartOn {
		return
	}
	if err := enableAutoStart(); err != nil {
		logWarn("自启动任务已存在，但按当前路径重写失败，开机启动可能仍指向旧位置：%v", err)
		return
	}
	logDebug("自启动任务已确认指向当前程序")
}

// enableAutoStart 创建/覆盖自启动任务，使用任务计划程序以最高权限运行。
//
// /TR 的路径必须自带引号：默认可执行文件位于用户目录下，而用户名允许包含
// 空格（如 C:\Users\John Doe\...），不加引号时 schtasks 会把空格后的部分
// 当成传入参数，任务会指向一个不存在的路径。
func enableAutoStart() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位自身可执行文件失败: %w", err)
	}
	cmd := exec.Command("schtasks", "/create", "/TN", taskName, "/TR", `"`+exe+`"`,
		"/SC", "ONLOGON", "/RL", "HIGHEST", "/DELAY", "0000:10", "/F")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("创建自启动任务失败: %v\n%s", err, string(out))
	}
	clearExecutionTimeLimit()
	return nil
}

// clearExecutionTimeLimit 把任务的执行时限设为无限。
//
// 任务计划程序默认会在任务启动 72 小时后强行停止它（TaskSettings.ExecutionTimeLimit
// 的官方说明：默认 72 小时，PT0S 表示不限制）。VRTX 是常驻程序，被这样停掉时进程
// 直接消失：Go 侧没有退出日志，AutoHotkey 的父进程看门狗随即结束脚本，用户只看到
// 托盘图标不见、快捷键失效，事后在任务计划程序里也只有一个终止码。
// schtasks /create 没有设置该时限的参数，只能用 PowerShell 补这一步。
// 这一步失败不影响自启动本身，因此只记警告。
func clearExecutionTimeLimit() {
	script := `
$task = Get-ScheduledTask -TaskName '` + taskName + `' -ErrorAction Stop
if ($task.Settings.ExecutionTimeLimit -ne 'PT0S') {
    $task.Settings.ExecutionTimeLimit = 'PT0S'
    Set-ScheduledTask -TaskName '` + taskName + `' -Settings $task.Settings | Out-Null
}
`
	cmd := powershell(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		logWarn("设置自启动任务执行时限失败，任务仍会在启动 72 小时后被强行停止：%v\n%s", err, out)
	}
}

// disableAutoStart 删除自启动任务（不存在视为成功）
func disableAutoStart() error {
	cmd := exec.Command("schtasks", "/delete", "/TN", taskName, "/F")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("删除自启动任务失败: %v\n%s", err, string(out))
	}
	return nil
}
