package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// main 是程序入口，执行流程：
//  1. --version/-v 打印版本后退出
//  2. 加载 exe 同目录的 vrtx.json（不存在则生成默认配置）
//  3. 清空并重建输出目录
//  4. 进入托盘模式：按配置执行提取与监控，行为变更全部通过网页设置面板或配置文件完成
func main() {
	// 在其余逻辑前优先处理 --version / -v，打印版本与构建时间后直接退出，
	// 版本号与构建时间由发布构建通过链接参数注入，详见 version.go
	if hasVersionFlag() {
		logInfo("VRTX v%s (build %s)", Version, BuildTime)
		return
	}

	// 单实例守卫必须最先执行：后续会读写 vrtx.json 和输出目录，
	// 若放到配置加载之后，第二个实例会用陈旧内容覆盖第一个实例刚保存的设置
	if !acquireSingleInstance() {
		logError("VRTX 已在运行，请勿重复启动")
		return
	}

	// 加载配置：唯一的行为控制来源（替代原命令行参数）
	initConfig()

	outputDir := current().OutputPath()

	// 写入侧底线：配置的输出目录必须为空/不存在/纯 VRTX 内容，否则拒绝启动
	if err := ensureOwnedDir(outputDir); err != nil {
		logFatal("输出目录不可用：%s\n%v\n请更换 output_dir 配置，或手动清理该目录后重试。", outputDir, err)
	}

	// 每次启动先清空输出目录（凭所有权校验），避免残留文件干扰增量结果
	if err := removeOwnedDir(outputDir); err != nil {
		logFatal("无法清理输出目录：%v", err)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		logFatal("无法创建输出目录: %v", err)
	}
	markOwned(outputDir)

	// 通过 context 协调后台监控生命周期，SIGINT/SIGTERM 同样触发退出
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	logInfo("VRTX 输出目录: %s", outputDir)

	// 先声明 DPI 感知，再进入托盘模式（创建托盘菜单前），避免菜单文字模糊
	setDPIAware()

	runTray(ctx, cancel)
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
