//go:build windows

package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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
	// 首屏页面：日志视图 + 设置面板（hash 路由 #settings），通过 EventSource 实时接收日志
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, consoleHTML)
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
			io.WriteString(w, "data: "+entryJSON(e)+"\n\n")
			flusher.Flush()
		}

		ch, unsub := ring.subscribe()
		defer unsub()
		for {
			select {
			case <-r.Context().Done():
				return
			case e := <-ch:
				if _, err := io.WriteString(w, "data: "+entryJSON(e)+"\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})
	// 配置读取/修改：GET 返回三件套（当前值/默认值/修改标记），POST 校验后热生效并落盘
	mux.HandleFunc("/api/config", apiConfigHandler)
	// 清理输出：清空输出目录并按当前配置立即重建（内部走所有权守卫）
	mux.HandleFunc("/api/clean", apiCleanHandler)
	// 未知 API 路径返回 404 JSON，绝不落进 "/" 的 HTML 兜底——
	// 否则前端会把页面当配置解析，一切失败都无声无息
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]any{"error": "unknown api endpoint"})
	})

	logServer = &http.Server{Handler: mux}
	go logServer.Serve(ln)

	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	logInfo("网页控制台已启动：%s", url)
	return url, nil
}

// apiConfigHandler 提供配置的读取与修改。
// POST 时服务端统一校验（含输出目录写入侧准入），不合规直接拒绝且不落盘不生效。
func apiConfigHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cur := current()
		def := defaultConfig()
		writeJSON(w, map[string]any{
			"config":   cur,
			"defaults": def,
			"modified": modifiedFields(cur, def),
		})
	case http.MethodPost:
		if !sameOrigin(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var nc Config
		if err := json.Unmarshal(body, &nc); err != nil {
			http.Error(w, "无效的 JSON："+err.Error(), http.StatusBadRequest)
			return
		}
		nc.sanitize()
		if err := ensureOwnedDir(nc.OutputPath()); err != nil {
			http.Error(w, "输出目录不可用："+err.Error(), http.StatusBadRequest)
			return
		}
		updateConfig(&nc)
		writeJSON(w, map[string]any{"ok": true})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// apiCleanHandler 触发「清理并重建」；实际清理走所有权守卫，失败会记入日志流
func apiCleanHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	go cleanAndRebuild()
	writeJSON(w, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// sameOrigin 校验请求来源与服务器同源，防止其他网页借用户浏览器跨站调用本机 API；
// 无 Origin 头的请求视为非浏览器客户端，直接放行
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

// startLogServerSingleton 保证网页控制台只启动一次，后续调用复用同一 URL
func startLogServerSingleton() (string, error) {
	logServerOnce.Do(func() {
		logServerURL, logServerErr = startLogServer()
	})
	return logServerURL, logServerErr
}

// openConsole 懒启动网页控制台（仅一次）并在默认浏览器中打开。
// fragment 用于直达设置面板（如 "#settings"，hash 路由见前端）。
func openConsole(fragment string) {
	urlStr, err := startLogServerSingleton()
	if err != nil {
		logError("启动网页控制台失败：%v", err)
		return
	}
	cmd := exec.Command("cmd", "/c", "start", "", urlStr+fragment)
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
