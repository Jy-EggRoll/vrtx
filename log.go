package main

import (
	"fmt"
	"os"
	"time"
)

// ANSI escape code 用于终端彩色输出。
// Windows 下需 Windows Terminal / ConEmu 等支持 ANSI 的终端模拟器。
const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	gray   = "\033[90m"
)

func logInfo(format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	fmt.Fprintf(os.Stdout, "%s%s%s %s%s%s\n", gray, time.Now().Format("15:04:05"), reset, green, msg, reset)
}

func logWarn(format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	fmt.Fprintf(os.Stderr, "%s%s%s %s%s%s\n", gray, time.Now().Format("15:04:05"), reset, yellow, msg, reset)
}

func logError(format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	fmt.Fprintf(os.Stderr, "%s%s%s %s%s%s\n", gray, time.Now().Format("15:04:05"), reset, red, msg, reset)
}

func logFatal(format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	fmt.Fprintf(os.Stderr, "%s%s%s %s%s%s\n", gray, time.Now().Format("15:04:05"), reset, bold+red, msg, reset)
	os.Exit(1)
}
