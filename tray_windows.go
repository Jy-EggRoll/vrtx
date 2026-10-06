//go:build windows

package main

import (
	"context"
	_ "embed"
	"os"
	"os/exec"
	"sync/atomic"

	"github.com/lutischan-ferenc/systray"
	"golang.org/x/sys/windows"
)

//go:embed assets/icon.ico
var iconData []byte

// setDPIAware 让进程声明 DPI 感知，避免系统拉伸托盘菜单导致文字模糊。
// 必须在创建任何窗口（含托盘菜单）之前调用；优先使用 Per-Monitor V2，
// 旧系统回退到 SetProcessDPIAware（系统级 DPI 感知）。
func setDPIAware() {
	user32 := windows.NewLazySystemDLL("user32.dll")
	if p := user32.NewProc("SetProcessDpiAwarenessContext"); p.Find() == nil {
		// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = (HANDLE)-4；64 位下即 0xFFFF…FC（用 ^uintptr(3) 表示，避免负常量大转 uintptr 溢出）
		p.Call(^uintptr(3))
		return
	}
	if p := user32.NewProc("SetProcessDPIAware"); p.Find() == nil {
		p.Call()
	}
}

// singleInstanceName 是跨实例互斥体名称，避免双击 exe 多次产生多个托盘图标
const singleInstanceName = `Global\VRTX-SingleInstance`

// singleInstanceHandle 保存互斥体句柄，供重启时手动释放
var singleInstanceHandle uintptr

// restarting 标记本次退出属于“重启”而不是“退出”。重启时输出目录必须原样
// 留给新实例：新实例会在同一目录里重新创建内容，父进程这时再清理一遍，
// 删掉的正是新实例刚写好的条目。而监控循环只在源目录发生变化时才重建，
// 不会因为输出目录被清空而自行恢复，用户看到的是跳转列表变空且长期不恢复。
var restarting atomic.Bool

// acquireSingleInstance 尝试创建命名互斥体，若已存在说明已有实例在运行，返回 false
func acquireSingleInstance() bool {
	h, err := windows.CreateMutex(nil, true, windows.StringToUTF16Ptr(singleInstanceName))
	if err == nil {
		singleInstanceHandle = uintptr(h)
		return true
	}
	if err == windows.ERROR_ALREADY_EXISTS {
		return false
	}
	// 其他错误（如权限不足）也视为无法获取单实例
	return false
}

// releaseSingleInstance 手动释放互斥体，使新实例可以获取所有权
func releaseSingleInstance() {
	if singleInstanceHandle != 0 {
		windows.CloseHandle(windows.Handle(singleInstanceHandle))
		singleInstanceHandle = 0
	}
}

// restart 退出当前进程并启动新实例，实现托盘菜单的重启功能
func restart() {
	exe, err := os.Executable()
	if err != nil {
		logWarn("无法获取可执行文件路径：%v", err)
		return
	}
	// 标记本次退出是重启：输出目录要留给新实例，退出流程不清理它
	restarting.Store(true)

	// 必须先释放单实例互斥体，再启动新进程：新实例一进来就会尝试获取同一个
	// 互斥体，只要它发现互斥体已存在，就会把自己当成“重复启动”直接退出，
	// 结果是“重启”退化成“只是关掉了”。原先的顺序正好踩中这个窗口。
	releaseSingleInstance()

	// 释放互斥体到新实例取得它之间有极短的窗口：此时用户双击 exe 或由启动项
	// 再次拉起，就能成功占住互斥体并常驻，出现两个托盘图标。这个窗口要靠跨进程
	// 交接才能彻底消除，为此引入一套握手协议并不划算，这里接受它足够短。
	// 下面只在 Start 返回错误时恢复守卫：新实例若启动成功却随即自己退出，
	// 本进程无从得知，用户需要再启动一次。
	cmd := exec.Command(exe)
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		logWarn("无法启动新实例：%v", err)
		// 启动失败就重新占住互斥体，否则本进程会处于“没人守门”的状态，
		// 之后用户再双击 exe 就能起出第二个实例
		if !acquireSingleInstance() {
			logError("重新获取单实例互斥体失败，重启后可能出现双实例")
		}
		return
	}
	systray.Quit()
}

// runTray 进入系统托盘模式：显示图标与菜单，后台运行提取+监控，直到用户退出。
// 行为配置全部来自全局配置快照（vrtx.json / 网页设置面板），不再经参数传入。
func runTray(ctx context.Context, cancel context.CancelFunc) {
	systray.Run(func() {
		systray.SetIcon(iconData)
		systray.SetTitle("VRTX")
		systray.SetTooltip("VRTX 运行中")

		mConsole := systray.AddMenuItem("打开控制台", "在浏览器中查看实时日志")
		mSettings := systray.AddMenuItem("打开设置", "在浏览器中调整运行行为")
		mAuto := systray.AddMenuItemCheckbox("开机自动启动", "在系统启动时自动运行 VRTX", detectAutoStart() == autostartOn)
		mRestart := systray.AddMenuItem("重启", "退出并重新启动 VRTX")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "退出 VRTX")

		// 单击托盘图标即打开网页控制台（按用户习惯：单击开控制台）
		systray.SetOnClick(func(menu systray.IMenu) { openConsole("") })

		// 菜单点击事件：控制台 / 设置 / 自启开关 / 重启 / 退出
		mConsole.Click(func() { openConsole("") })
		mSettings.Click(func() { openConsole("#settings") })
		mAuto.Click(func() { toggleAutoStart(mAuto) })
		mRestart.Click(func() { restart() })
		mQuit.Click(func() { systray.Quit() })

		// 后台执行首次提取并进入监控循环，避免阻塞托盘消息循环；
		// 提取范围与监控行为均实时读取配置快照
		go func() {
			// 提取与监控都在这个 goroutine 里。panic 不会跨 goroutine 传播，
			// 缺了这道保护就会直接带走整个进程：托盘图标消失，日志里也没有任何记录
			defer recoverLog("后台监控")

			// 自启动任务若仍指向旧路径（升级或更换安装位置之后），在这里修正
			repairAutoStartTask()

			runFullExtract(getOutputDir())
			logDebug("首次提取完成，进入监控模式（输出目录：%s）", getOutputDir())

			if current().AHK {
				if err := launchAHK(); err != nil {
					logDebug("未启动 AutoHotkey：%v", err)
				} else {
					logDebug("已拉起 AutoHotkey 脚本（管理员权限，随本程序退出自动结束）")
				}
			}

			startWatch(ctx)
		}()
	}, func() {
		// 退出流程里同样可能 panic，而且此刻正在做清理，更需要留下痕迹
		defer recoverLog("退出流程")

		// 退出标记：与启动标记配对。它必须写在任何清理动作之前，
		// 否则清理一旦出错提前返回，日志里就只剩启动、没有退出，
		// 事后会被误判成“进程被外部终止”。
		logInfo("VRTX 开始退出，PID %d", os.Getpid())

		// 退出时停止监控并关闭网页控制台服务
		cancel()
		if logServer != nil {
			// 立即关闭所有连接（含 SSE 长连接），不等待浏览器断开，否则 Shutdown 会阻塞到网页关闭才返回
			_ = logServer.Close()
		}
		// 退出时清理输出目录，不留任何痕迹；重启时跳过，目录要留给新实例
		if !restarting.Load() {
			if err := os.RemoveAll(getOutputDir()); err != nil {
				logWarn("退出时清理输出目录失败：%v", err)
			}
		}
	})
}
