//go:build windows

package main

import (
	"fmt"
	"html"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
)

// logServer 是网页控制台 HTTP 服务实例，退出时用于关闭
var logServer *http.Server

var (
	logServerOnce sync.Once
	logServerURL  string
	logServerErr  error
)

// startLogServer 在 127.0.0.1 的随机空闲端口启动网页控制台，返回访问 URL。
// 端口写 0 由系统分配，再读回实际端口，每次启动都是不同的随机端口，不写死。
func startLogServer() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	port := ln.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, renderConsoleHTML())
	})

	logServer = &http.Server{Handler: mux}
	go logServer.Serve(ln)

	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	logInfo("网页控制台已启动：%s", url)
	return url, nil
}

// startLogServerSingleton 保证网页控制台只启动一次，后续调用复用同一 URL
func startLogServerSingleton() (string, error) {
	logServerOnce.Do(func() {
		logServerURL, logServerErr = startLogServer()
	})
	return logServerURL, logServerErr
}

// openConsole 懒启动网页控制台（仅一次）并在默认浏览器中打开
func openConsole() {
	url, err := startLogServerSingleton()
	if err != nil {
		logError("启动网页控制台失败：%v", err)
		return
	}
	cmd := exec.Command("cmd", "/c", "start", "", url)
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		logError("打开浏览器失败：%v", err)
	}
}

// renderConsoleHTML 生成多彩的网页控制台页面，按日志级别着色，每 1 秒自动刷新
func renderConsoleHTML() string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="zh"><head><meta charset="utf-8">`)
	b.WriteString(`<meta http-equiv="refresh" content="1">`)
	b.WriteString(`<title>VRTX 控制台</title>`)
	b.WriteString(`<style>`)
	b.WriteString(`body{background:#0d1117;color:#c9d1d9;font-family:Consolas,Menlo,monospace;font-size:13px;margin:0;padding:10px}`)
	b.WriteString(`#hint{position:fixed;top:0;right:10px;color:#6e7681}`)
	b.WriteString(`.ts{color:#8b949e}.info{color:#3fb950}.warn{color:#d29922}.error{color:#f85149}.fatal{color:#f85149;font-weight:bold}`)
	b.WriteString(`a{color:#58a6ff}`)
	b.WriteString(`</style></head><body>`)
	b.WriteString(`<div id="hint">VRTX 控制台 · 每 1 秒刷新</div>`)

	levelClass := map[logLevel]string{
		levelInfo:  "info",
		levelWarn:  "warn",
		levelError: "error",
		levelFatal: "fatal",
	}
	for _, e := range ring.snapshot() {
		cls := levelClass[e.Level]
		b.WriteString(`<div><span class="ts">`)
		b.WriteString(e.Time.Format("15:04:05"))
		b.WriteString(`</span> <span class="`)
		b.WriteString(cls)
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(e.Msg))
		b.WriteString(`</span></div>`)
	}
	b.WriteString(`</body></html>`)
	return b.String()
}
