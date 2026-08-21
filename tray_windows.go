//go:build windows

package main

import (
	"context"
	_ "embed"
	"time"

	"github.com/lutischan-ferenc/systray"
	"golang.org/x/sys/windows"
)

//go:embed assets/icon.ico
var iconData []byte

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

// runTray 进入系统托盘模式：显示图标与菜单，后台运行提取+监控，直到用户退出
func runTray(ctx context.Context, cancel context.CancelFunc, outputDir string, interval time.Duration, watch, bookmarks, software, system, drives, recent, office bool) {
	systray.Run(func() {
		systray.SetIcon(iconData)
		systray.SetTitle("VRTX")
		systray.SetTooltip("VRTX 运行中")

		mConsole := systray.AddMenuItem("打开控制台输出", "在浏览器中查看实时日志")
		mQuit := systray.AddMenuItem("退出", "退出 VRTX")

		// 单击托盘图标即打开网页控制台（按用户习惯：单击开控制台）
		systray.SetOnClick(func(menu systray.IMenu) { openConsole() })

		// 菜单点击事件：打开控制台 / 退出
		mConsole.Click(func() { openConsole() })
		mQuit.Click(func() { systray.Quit() })

		// 后台执行首次提取并进入监控循环，避免阻塞托盘消息循环
		go func() {
			if bookmarks {
				logInfo("正在提取书签...")
				extractBookmarks(outputDir)
			}
			if software || system || drives || recent || office {
				logInfo("正在提取快捷方式...")
				extractShortcuts(outputDir, software, system, drives, recent, office)
			}
			logInfo("首次提取完成，进入监控模式（输出目录：%s）", outputDir)

			if watch {
				startWatch(ctx, outputDir, interval, bookmarks, software, system, drives, recent, office)
			} else {
				logInfo("已按 --watch=false 仅提取一次，托盘保持运行")
			}
		}()
	}, func() {
		// 退出时停止监控并关闭网页控制台服务
		cancel()
		if logServer != nil {
			_ = logServer.Shutdown(context.Background())
		}
	})
}
