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
// 这里不走 schtasks /create，而是用 PowerShell 的 ScheduledTasks 模块一次性提交整份
// 任务定义，原因是 schtasks 关不掉两项电池相关的默认设置：
//   - DisallowStartIfOnBatteries（默认开启）会让笔记本在电池供电时登录根本不启动本任务，
//     任务计划程序只留下事件 326（NO_START_ON_BATTERIES）；
//   - StopIfGoingOnBatteries（默认开启）会在运行中切到电池时把常驻的 VRTX 直接终止，
//     对应事件 327。
//
// 这两项在 schtasks 命令行里没有任何开关可以覆盖，而 ScheduledTasks 模块能直接给出
// New-ScheduledTaskSettingsSet 的 -AllowStartIfOnBatteries / -DontStopIfGoingOnBatteries。
// 顺带地，执行时限也能在注册时就设成无限，原先为了补这一项而跑的第二次 PowerShell
// （clearExecutionTimeLimit）不再需要。
//
// 可执行文件路径用环境变量传给脚本，而不是拼进脚本文本：安装路径可能含空格、单引号等
// 字符，拼字符串就得处理转义，走环境变量可以完全绕开。
func enableAutoStart() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位自身可执行文件失败: %w", err)
	}
	// $ErrorActionPreference='Stop'：让任意一个 cmdlet 失败都变成终止性错误，
	// 从而以非零退出码体现出来；否则脚本会继续往后跑，失败被淹没在输出里。
	script := `
$ErrorActionPreference = 'Stop'
$exe = $env:VRTX_TASK_EXE
$user = "$env:USERDOMAIN\$env:USERNAME"

# 动作：就是当前这个可执行文件，不带参数
$action = New-ScheduledTaskAction -Execute $exe

# 触发器：当前用户登录时启动，延迟 3 秒（PT03S）。这个延迟与 TrafficMonitor 的实现对齐，
# 不再沿用原来 schtasks 的 /DELAY 0000:10。New-ScheduledTaskTrigger 本身没有延迟参数，
# 只能在返回的触发器对象上写 Delay 属性
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $user
$trigger.Delay = 'PT03S'

# 主体：当前用户。LogonType Interactive 表示复用已登录会话的交互式令牌，因此不需要密码；
# RunLevel Highest 对应原来的 /RL HIGHEST
$principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Highest

# 设置只显式给三项：允许电池供电时启动、切到电池后不停止（见函数开头的说明），
# 以及执行时限 0 即 PT0S（不限制运行时长，替代原先单独设置时限的那趟调用）。
# 其余保持默认：多实例策略的默认值就是 IgnoreNew，不重复启动，与原先依赖
# schtasks 默认值时的行为一致，因此不传 -MultipleInstances。
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit (New-TimeSpan -Seconds 0)

# -Force：任务已存在时不询问确认，直接覆盖，保证注册是幂等的
Register-ScheduledTask -TaskName '` + taskName + `' -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null
`
	cmd := powershell(script)
	cmd.Env = append(os.Environ(), "VRTX_TASK_EXE="+exe)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("创建自启动任务失败: %v\n%s", err, string(out))
	}
	return nil
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
