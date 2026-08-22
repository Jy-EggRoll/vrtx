//go:build windows

package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"sync"
)

//go:embed web/console.html
var consoleHTML string

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
	// 首屏页面：通过 EventSource 订阅 /stream 实时接收日志
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, consoleHTML)
	})
	// SSE 实时日志流：先回放历史，再持续推送新增日志
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		// 先回放当前缓冲，保证打开页面即有历史
		for _, e := range ring.snapshot() {
			fmt.Fprintf(w, "data: %s\n\n", entryJSON(e))
			flusher.Flush()
		}

		ch, unsub := ring.subscribe()
		defer unsub()
		for {
			select {
			case <-r.Context().Done():
				return
			case e := <-ch:
				if _, err := fmt.Fprintf(w, "data: %s\n\n", entryJSON(e)); err != nil {
					return
				}
				flusher.Flush()
			}
		}
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

// entryJSON 将一条日志序列化为 SSE 载荷（时间已格式化、消息转义由浏览器处理）
func entryJSON(e logEntry) string {
	b, _ := json.Marshal(struct {
		Level string `json:"level"`
		Time  string `json:"time"`
		Msg   string `json:"msg"`
	}{Level: string(e.Level), Time: e.Time.Format("15:04:05"), Msg: e.Msg})
	return string(b)
}
