package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// logLevel 是日志级别，网页控制台据此为每条日志着色。
//
// 级别的语义边界（新增调用点时按此选择，不要随手挑一个）：
//   - trace：逐个文件的操作明细，例如复制了哪个快捷方式、生成了哪个书签
//   - debug：汇总与中间状态，例如一次重建新建了多少项、新建明细
//   - info：用户可感知的进展，例如正在提取、监控已启动、配置已变更
//   - warn：已降级处理的问题，程序继续正常运行
//   - error：操作失败但程序继续运行
//   - fatal：失败且程序必须退出
type logLevel string

const (
	levelTrace logLevel = "trace"
	levelDebug logLevel = "debug"
	levelInfo  logLevel = "info"
	levelWarn  logLevel = "warn"
	levelError logLevel = "error"
	levelFatal logLevel = "fatal"
)

// defaultLogLevel 是首次运行与回退时使用的级别。
// 这里是唯一定义处：defaultConfig、非法值回退、日志初始化都从这里取值，
// 避免“默认级别”散落在多个文件里各写一份。
//
// 取 info 的理由：进程的启动与退出标记是排查“程序无故消失”的唯一线索，
// 必须在这一档就能看到。用户不关心的运行细节（提取了哪些文件、配置放在
// 哪里）一律降到 debug 或 trace，不占用 info 这一档。
const defaultLogLevel = levelInfo

// levelRanks 把级别映射为可比较的序号，用于阈值过滤。
// web/console.html 的 RANK 表必须与这里一致，改动时两处同步。
var levelRanks = map[logLevel]int{
	levelTrace: 0,
	levelDebug: 1,
	levelInfo:  2,
	levelWarn:  3,
	levelError: 4,
	levelFatal: 5,
}

// parseLevel 解析配置里的级别字符串，返回规范化后的级别与是否合法。
// 容错大小写与首尾空白，便于用户手工编辑 vrtx.json。
func parseLevel(s string) (logLevel, bool) {
	lv := logLevel(strings.ToLower(strings.TrimSpace(s)))
	if _, ok := levelRanks[lv]; !ok {
		return "", false
	}
	return lv, true
}

// logEntry 是结构化日志条目，同时供网页控制台着色与内存环形缓冲使用
type logEntry struct {
	Level logLevel
	Time  time.Time
	Msg   string
}

// logRing 是线程安全、有界的内存日志环形缓冲；超出容量时丢弃最旧条目，避免溢出。
// 同时维护一组订阅者通道，新日志会实时广播给订阅者（供网页控制台 SSE 使用）。
// 字段名用 size 而不是 cap，避免遮蔽内建函数 cap 造成阅读时的歧义。
type logRing struct {
	mu   sync.Mutex
	buf  []logEntry
	size int
	subs map[chan logEntry]struct{}
}

func newLogRing(size int) *logRing {
	return &logRing{size: size, subs: make(map[chan logEntry]struct{})}
}

// append 写入一条日志，超出容量时从队首丢弃最旧条目，并向所有订阅者广播
func (r *logRing) append(e logEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, e)
	if len(r.buf) > r.size {
		r.buf = r.buf[len(r.buf)-r.size:]
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

// ring 是全局日志缓冲，网页控制台读取它；
// 容量 1000 条，满了就丢弃最旧条目，不会无限增长。
// 注意它与落盘日志的范围并不完全相同：文件保留全部（直到轮转），
// 这里只保留最近 1000 条，所以排查很久以前的问题要看文件。
var ring = newLogRing(1000)

const (
	// logFileName 是落盘日志的文件名，位于 %LOCALAPPDATA%\VRTX 下。
	// 不放 %TEMP%\VRTX：那个目录在程序退出时会被整个删除，日志留不下来。
	// 而日志的首要用途恰恰是回答“程序上次为什么没了”，必须比进程活得久。
	logFileName = "vrtx.log"

	// maxLogFileBytes 是单份日志文件的大小上限，超过后轮转为 vrtx.log.1。
	// 只保留一代备份，旧的那份被覆盖，避免长期运行时无限增长。
	maxLogFileBytes = 1 << 20
)

var (
	// logFilePath 为空表示文件日志不可用（目录不可写、无法定位用户数据目录等），
	// 此时程序只保留内存日志，其余功能不受影响。
	logFilePath string

	// logFileSize 是当前日志文件的近似大小，用于判断是否需要轮转。
	// 由 logFileMu 保护。
	logFileSize int64

	// logFileMu 串行化文件写入。环形缓冲有各自的锁，但文件句柄是共享资源，
	// 且 O_APPEND 在 Windows 上不是原子追加，多 goroutine 并发写会交错。
	logFileMu sync.Mutex

	// logThreshold 保存当前生效的输出阈值（logLevel）。
	// 用 atomic.Value 是因为网页设置面板可以在任意时刻改它，
	// 而各个 goroutine 正在同时读它。
	logThreshold atomic.Value
)

// initLog 确定日志文件位置并准备落盘。必须在任何 logXxx 之前调用。
// 定位顺序：%LOCALAPPDATA%（Windows 上即 os.UserCacheDir）→ exe 同目录。
// 两者都不可用时只保留内存日志：日志不该成为新的故障来源。
func initLog() {
	// 配置还没读出来，先按默认级别过滤，加载配置后由 setLogLevel 覆盖
	logThreshold.Store(defaultLogLevel)

	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		exe, exeErr := os.Executable()
		if exeErr != nil {
			return
		}
		dir = filepath.Dir(exe)
	}

	logDir := filepath.Join(dir, "VRTX")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return
	}

	logFilePath = filepath.Join(logDir, logFileName)
	if fi, statErr := os.Stat(logFilePath); statErr == nil {
		logFileSize = fi.Size()
	}
}

// setLogLevel 应用配置中的日志级别；非法值回退为默认级别。
// 调用方是网页面板的保存动作与启动时的配置加载，两者都要求“立即生效”。
func setLogLevel(s string) {
	lv, ok := parseLevel(s)
	if !ok {
		lv = defaultLogLevel
	}
	logThreshold.Store(lv)
}

// currentLogLevel 返回当前输出阈值，未初始化时按默认级别处理
func currentLogLevel() logLevel {
	if v, ok := logThreshold.Load().(logLevel); ok {
		return v
	}
	return defaultLogLevel
}

// logAt 是所有日志函数的唯一出口：先按阈值过滤，再同时写入内存缓冲与文件。
// 因为过滤发生在写入之前，所以提高级别会同时减少文件与网页控制台的内容，
// 控制台里的“全部”实际含义是“不低于当前级别”。
//
// skip 传给 runtime.Caller，用来定位调用点，各 logXxx 固定传 3。调用链是
// logXxx → logAt → writeLogFile，而 runtime.Caller 在 writeLogFile 内部调用，
// 于是栈深度依次为：
//
//	0 = writeLogFile，1 = logAt，2 = logXxx，3 = 调用 logXxx 的用户代码
//
// 这个数字与调用层级强耦合，日后若在 logXxx 与 logAt 之间再加一层包装，
// 必须同步调整，否则日志里的位置会指向本文件而不是出问题的地方。
func logAt(level logLevel, skip int, format string, v ...any) {
	if levelRanks[level] < levelRanks[currentLogLevel()] {
		return
	}

	msg := fmt.Sprintf(format, v...)
	now := time.Now()

	ring.append(logEntry{Level: level, Time: now, Msg: msg})
	writeLogFile(now, level, msg, skip)
}

// writeLogFile 把一条日志追加到文件。任何失败都静默放弃：
// 记录日志这件事本身不应该再产生错误，更不该让调用方处理。
func writeLogFile(t time.Time, level logLevel, msg string, skip int) {
	if logFilePath == "" {
		return
	}

	loc := "?"
	if pc, file, line, ok := runtime.Caller(skip); ok {
		name := "?"
		if fn := runtime.FuncForPC(pc); fn != nil {
			// 完整函数名形如 main.extractBookmarks，这里只保留函数名本身，
			// 因为包名恒为 main，留着只是占位置
			full := fn.Name()
			if idx := strings.LastIndex(full, "."); idx >= 0 {
				full = full[idx+1:]
			}
			name = full
		}
		loc = fmt.Sprintf("%s:%d %s", filepath.Base(file), line, name)
	}

	text := fmt.Sprintf("%s [%-5s] %s | %s\n",
		t.Format("2006-01-02 15:04:05.000"),
		strings.ToUpper(string(level)),
		loc,
		msg,
	)

	logFileMu.Lock()
	defer logFileMu.Unlock()

	if logFileSize >= maxLogFileBytes {
		// 轮转失败就继续往原文件追加：丢日志比文件超限严重得多
		if err := os.Rename(logFilePath, logFilePath+".1"); err == nil {
			logFileSize = 0
		}
	}

	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	if n, err := f.WriteString(text); err == nil {
		logFileSize += int64(n)
	}
}

func logTrace(format string, v ...any) { logAt(levelTrace, 3, format, v...) }
func logDebug(format string, v ...any) { logAt(levelDebug, 3, format, v...) }
func logInfo(format string, v ...any)  { logAt(levelInfo, 3, format, v...) }
func logWarn(format string, v ...any)  { logAt(levelWarn, 3, format, v...) }
func logError(format string, v ...any) { logAt(levelError, 3, format, v...) }

// logFatal 记录致命错误并结束进程。logAt 是同步写文件，
// 所以这里返回时内容已经落到磁盘上，不存在“日志还没写完就退出”。
func logFatal(format string, v ...any) {
	logAt(levelFatal, 3, format, v...)
	os.Exit(1)
}

// recoverLog 捕获 panic 并把堆栈写进日志，供各 goroutine 用 defer 调用。
//
// 之所以需要在每个后台 goroutine 里单独 defer，而不是只靠 main 的那一层：
// Go 的 panic 不会跨 goroutine 传播，任何一个 goroutine 崩溃都会直接终止
// 整个进程。本程序的后台 goroutine 正是提取与监控的主力，一旦其中有 bug，
// 用户看到的现象就是“程序无缘无故消失了”，而堆栈早已随进程一起丢掉。
// 这里捕获后只记录、不退出，让其余部分继续工作。
func recoverLog(scope string) {
	if r := recover(); r != nil {
		logError("goroutine [%s] 发生 panic: %v\n%s", scope, r, debug.Stack())
	}
}
