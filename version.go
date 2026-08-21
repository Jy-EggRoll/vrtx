package main

import "os"

// Version 由发布构建通过链接参数注入；本地开发构建保留 dev
var Version = "dev"

// BuildTime 由发布构建通过链接参数注入；本地开发构建保留 unknown
var BuildTime = "unknown"

// hasVersionFlag 检查命令行参数中是否包含 --version 或 -v，
// 用于在正式解析 flags 之前提前退出并打印版本信息
func hasVersionFlag() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			return true
		}
	}
	return false
}
