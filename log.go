package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// ANSI escape code 用于终端彩色输出。
// Windows 下需 Windows Terminal / ConEmu 等支持 ANSI 的终端模拟器；GUI 构建无控制台时静默丢弃。
const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	gray   = "\033[90m"
)

// logLevel 是日志级别，网页控制台据此为每条日志着色
type logLevel string

const (
	levelInfo  logLevel = "info"
	levelWarn  logLevel = "warn"
	levelError logLevel = "error"
	levelFatal logLevel = "fatal"
)

// logEntry 是结构化日志条目，同时供网页控制台着色与内存环形缓冲使用
type logEntry struct {
	Level logLevel
	Time  time.Time
	Msg   string
}

// logRing 是线程安全、有界的内存日志环形缓冲；超出容量时丢弃最旧条目，避免溢出。
// 同时维护一组订阅者通道，新日志会实时广播给订阅者（供网页控制台 SSE 使用）。
type logRing struct {
	mu   sync.Mutex
	buf  []logEntry
	cap  int
	subs map[chan logEntry]struct{}
}

func newLogRing(cap int) *logRing {
	return &logRing{cap: cap, subs: make(map[chan logEntry]struct{})}
}

// append 写入一条日志，超出容量时从队首丢弃最旧条目，并向所有订阅者广播
func (r *logRing) append(e logEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, e)
	if len(r.buf) > r.cap {
		r.buf = r.buf[len(r.buf)-r.cap:]
	}
	for ch := range r.subs {
		// 订阅者若来不及消费则丢弃该条，避免阻塞主流程
		select {
		case ch <- e:
		default:
		}
	}
}

// snapshot 返回当前缓冲的副本，供网页控制台首屏渲染
func (r *logRing) snapshot() []logEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]logEntry, len(r.buf))
	copy(out, r.buf)
	return out
}

// subscribe 注册一个日志订阅者，返回接收通道与退订函数
func (r *logRing) subscribe() (chan logEntry, func()) {
	ch := make(chan logEntry, 256)
	r.mu.Lock()
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	unsub := func() {
		r.mu.Lock()
		delete(r.subs, ch)
		r.mu.Unlock()
	}
	return ch, unsub
}

// ring 是全局日志缓冲，网页控制台读取它；容量 4096 条，满了就砍最旧，不会溢出
var ring = newLogRing(4096)

// logWriter 把日志写入内存环形缓冲（供网页控制台），并以 ANSI 彩色文本写到控制台（若存在）
func logWriter(level logLevel, color, format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	ring.append(logEntry{Level: level, Time: time.Now(), Msg: msg})
	// GUI 构建下无控制台，fmt.Fprintf 到 os.Stdout 会静默丢弃，不报错
	fmt.Fprintf(os.Stdout, "%s%s%s %s%s%s\n", gray, time.Now().Format("15:04:05"), reset, color, msg, reset)
}

func logInfo(format string, v ...any) { logWriter(levelInfo, green, format, v...) }

func logWarn(format string, v ...any) { logWriter(levelWarn, yellow, format, v...) }

func logError(format string, v ...any) { logWriter(levelError, red, format, v...) }

func logFatal(format string, v ...any) {
	logWriter(levelFatal, bold+red, format, v...)
	os.Exit(1)
}
