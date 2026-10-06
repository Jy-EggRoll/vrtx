package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
)

// main 是程序入口，执行流程：
//  1. 初始化日志（内存缓冲 + %LOCALAPPDATA%\VRTX\vrtx.log）
//  2. 加载 exe 同目录的 vrtx.json（不存在则生成默认配置）
//  3. 清空并重建输出目录（固定为 %TEMP%\VRTX）
//  4. 进入托盘模式：按配置执行提取与监控，行为变更全部通过网页设置面板或配置文件完成
//
// 本程序是纯 GUI 程序，不提供任何命令行功能：以 windowsgui 构建的进程没有控制台，
// 输出无处可去。版本号改在网页控制台显示，见 console_windows.go 的 apiConfigHandler。
func main() {
	// 日志最先初始化：后面每一步（包括配置加载失败）都要能留下痕迹，
	// 在此之前发生的错误根本没有地方可记
	initLog()

	// 主流程的 panic 兜在这里。后台 goroutine 由 recoverLog 各自处理，
	// 这一层是最后的防线，捕获后按致命错误退出
	defer func() {
		if r := recover(); r != nil {
			logFatal("主流程发生 panic: %v\n%s", r, debug.Stack())
		}
	}()

	// 单实例守卫必须最先执行：后续会读写 vrtx.json 和输出目录，
	// 若放到配置加载之后，第二个实例会用陈旧内容覆盖第一个实例刚保存的设置
	if !acquireSingleInstance() {
		logError("VRTX 已在运行，请勿重复启动")
		return
	}

	// 加载配置：唯一的行为控制来源（替代原命令行参数）
	initConfig()

	// 日志级别必须在配置加载之后立刻生效，否则启动阶段的日志会全按默认级别输出
	setLogLevel(current().LogLevel)

	// 启动标记：与退出时的标记配对。日志里只有启动、没有对应的退出，
	// 就说明进程是被外部终止或崩溃掉的，而不是自己走完了退出流程
	logInfo("VRTX v%s (build %s) 启动，PID %d，日志级别 %s",
		Version, BuildTime, os.Getpid(), currentLogLevel())

	outputDir := getOutputDir()

	// 每次启动先清空输出目录，避免残留文件干扰增量结果
	if err := os.RemoveAll(outputDir); err != nil {
		logFatal("无法清理输出目录：%v", err)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		logFatal("无法创建输出目录: %v", err)
	}

	// 通过 context 协调后台监控生命周期，SIGINT/SIGTERM 同样触发退出
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer recoverLog("信号监听")
		<-sigCh
		cancel()
	}()

	logDebug("VRTX 输出目录: %s", outputDir)

	// 先声明 DPI 感知，再进入托盘模式（创建托盘菜单前），避免菜单文字模糊
	setDPIAware()

	runTray(ctx, cancel)
}

// getOutputDir 按 TEMP → TMP → AppData\Local\Temp 优先级 fallback 获取临时目录，
// 并在其下追加 VRTX 子目录作为输出路径。始终使用临时目录，不支持用户自定义。
func getOutputDir() string {
	tempDir := os.Getenv("TEMP")
	if tempDir == "" {
		tempDir = os.Getenv("TMP")
	}
	if tempDir == "" {
		homeDir, _ := os.UserHomeDir()
		tempDir = filepath.Join(homeDir, "AppData", "Local", "Temp")
	}
	return filepath.Join(tempDir, "VRTX")
}
