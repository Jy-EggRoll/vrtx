package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// main 是程序入口，执行流程：
//  1. 解析命令行参数
//  2. 如果 --clean 则清理输出目录后直接退出
//  3. 清空并重建输出目录
//  4. 提取书签和快捷方式（根据 flags）
//  5. 如果 --watch=true，进入监控循环（轮询检测变更并增量重建）
//  6. 如果 --watch=false，提取完成后直接退出
func main() {
	// 在解析其余参数前优先处理 --version / -v，打印版本与构建时间后直接退出，
	// 版本号与构建时间由发布构建通过链接参数注入，详见 version.go
	if hasVersionFlag() {
		logInfo("VRTX v%s (build %s)", Version, BuildTime)
		return
	}

	watch := flag.Bool("watch", true, "启用监控模式，检测到变更时自动重建")
	interval := flag.Duration("interval", 1*time.Second, "监控轮询间隔")
	outDir := flag.String("out", "", "输出目录（默认：系统临时目录下的 VRTX）")
	clean := flag.Bool("clean", false, "清除所有输出文件后退出")
	bookmarks := flag.Bool("bookmarks", true, "提取浏览器书签（Chrome / Edge）")
	software := flag.Bool("software", true, "提取软件快捷方式（开始菜单 / Windows Apps）")
	system := flag.Bool("system", true, "提取系统位置快捷方式（回收站 / 此电脑 / 用户目录 / 开机启动）")
	drives := flag.Bool("drives", true, "提取磁盘根目录快捷方式（C: D: ...）")
	recent := flag.Bool("recent", false, "提取 Windows 最近文件快捷方式")
	office := flag.Bool("office", false, "提取 Office 最近文件快捷方式")
	vscode := flag.Bool("vscode", true, "提取 VS Code 最近打开（本地 + 远程连接）")
	flag.Parse()

	outputDir := *outDir
	if outputDir == "" {
		outputDir = getOutputDir()
	}

	if *clean {
		if _, err := os.Stat(outputDir); os.IsNotExist(err) {
			logInfo("输出目录不存在，无需清理: %s", outputDir)
		} else {
			os.RemoveAll(outputDir)
			logInfo("已清除所有输出文件: %s", outputDir)
		}
		return
	}

	if !*bookmarks && !*software && !*system && !*drives && !*recent && !*office && !*vscode {
		logInfo("未启用任何提取功能，退出")
		return
	}

	// 每次启动先清空输出目录，避免上次残留文件干扰增量结果
	os.RemoveAll(outputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		logFatal("无法创建输出目录: %v", err)
	}

	// 单实例守卫：避免双击 exe 多次产生多个托盘图标（仅 Windows 有效）
	if !acquireSingleInstance() {
		logError("VRTX 已在运行，请勿重复启动")
		return
	}

	// 通过 context 协调后台监控生命周期，SIGINT/SIGTERM 同样触发退出
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	logInfo("Vortex 输出目录: %s", outputDir)

	// 先声明 DPI 感知，再进入托盘模式（创建托盘菜单前），避免菜单文字模糊
	setDPIAware()

	// 进入系统托盘模式：后台执行提取与监控，直到用户从托盘菜单退出
	runTray(ctx, cancel, outputDir, *interval, *watch, *bookmarks, *software, *system, *drives, *recent, *office, *vscode)
}

// getOutputDir 按 TEMP → TMP → AppData\Local\Temp 优先级 fallback 获取临时目录，
// 并在其下追加 VRTX 子目录作为默认输出路径。
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
