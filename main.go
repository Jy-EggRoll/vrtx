package main

import (
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	watch := flag.Bool("watch", true, "启用监控模式，检测到变更时自动重建")
	interval := flag.Duration("interval", 1*time.Second, "监控轮询间隔")
	outDir := flag.String("out", "", "输出目录（默认：系统临时目录下的 eggroll-vrtx）")
	clean := flag.Bool("clean", false, "清除所有输出文件后退出")
	bookmarks := flag.Bool("bookmarks", true, "提取浏览器书签（Chrome / Edge）")
	shortcuts := flag.Bool("shortcuts", true, "提取 Windows 快捷方式（开始菜单 / Windows Apps / 最近文件）")
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

	if !*bookmarks && !*shortcuts {
		logInfo("未启用任何提取功能，退出")
		return
	}

	os.RemoveAll(outputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		logFatal("无法创建输出目录: %v", err)
	}

	logInfo("Vortex 输出目录: %s", outputDir)

	if *bookmarks {
		logInfo("正在提取书签...")
		extractBookmarks(outputDir)
	}
	if *shortcuts {
		logInfo("正在提取快捷方式...")
		extractShortcuts(outputDir)
	}

	if !*watch {
		logInfo("提取完成！")
		return
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	startWatch(outputDir, *interval, sigCh, *bookmarks, *shortcuts)
}

func getOutputDir() string {
	tempDir := os.Getenv("TEMP")
	if tempDir == "" {
		tempDir = os.Getenv("TMP")
	}
	if tempDir == "" {
		homeDir, _ := os.UserHomeDir()
		tempDir = filepath.Join(homeDir, "AppData", "Local", "Temp")
	}
	return filepath.Join(tempDir, "eggroll-vrtx")
}
