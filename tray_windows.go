//go:build windows

package main

import (
	"context"
	_ "embed"

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

// acquireSingleInstance 尝试创建命名互斥体，若已存在说明已有实例在运行，返回 false
func acquireSingleInstance() bool {
	_, err := windows.CreateMutex(nil, true, windows.StringToUTF16Ptr(singleInstanceName))
	if err != nil {
		return false
	}
	// 若互斥体已存在，GetLastError 会返回 ERROR_ALREADY_EXISTS
	if windows.GetLastError() == windows.ERROR_ALREADY_EXISTS {
		return false
	}
	return true
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
		mAuto := systray.AddMenuItemCheckbox("开机自动启动", "在系统启动时自动运行 VRTX", autostartFileExists())
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "退出 VRTX")

		// 单击托盘图标即打开网页控制台（按用户习惯：单击开控制台）
		systray.SetOnClick(func(menu systray.IMenu) { openConsole("") })

		// 菜单点击事件：控制台 / 设置 / 自启开关 / 退出
		mConsole.Click(func() { openConsole("") })
		mSettings.Click(func() { openConsole("#settings") })
		mAuto.Click(func() { toggleAutoStart(mAuto) })
		mQuit.Click(func() { systray.Quit() })

		// 异步精化视觉状态：快速 Stat 只能判断"文件存在"，
		// 若实际是失效残留（指向旧路径），后台确认后取消对勾
		go func() {
			if detectAutoStart() != autostartOn {
				mAuto.Uncheck()
			}
		}()

		// 后台执行首次提取并进入监控循环，避免阻塞托盘消息循环；
		// 提取范围与监控行为均实时读取配置快照
		go func() {
			runFullExtract(current().OutputPath())
			logInfo("首次提取完成，进入监控模式（输出目录：%s）", current().OutputPath())
			startWatch(ctx)
		}()
	}, func() {
		// 退出时停止监控并关闭网页控制台服务
		cancel()
		if logServer != nil {
			// 立即关闭所有连接（含 SSE 长连接），不等待浏览器断开，否则 Shutdown 会阻塞到网页关闭才返回
			_ = logServer.Close()
		}
	})
}
