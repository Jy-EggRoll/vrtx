//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
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
	// 首屏页面：通过 EventSource 订阅 /stream 实时接收日志
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, renderConsoleHTML())
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

// renderConsoleHTML 生成猫普奇诺 Latte/Frappe 主题、自适应深浅色、卡片式布局的网页控制台页面。
// 字体优先微软雅黑，回退系统默认。
func renderConsoleHTML() string {
	return `<!doctype html><html lang="zh"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>VRTX 控制台</title>
<style>
:root {
  /* Catppuccin Latte (light) */
  --bg: #eff1f5;
  --surface: #e6e9ef;
  --card: #ffffff;
  --text: #4c4f69;
  --text-muted: #7c7f93;
  --border: #dce0e8;
  --primary: #1e66f5;
  --primary-hover: #1a5cd8;
  --info: #1e66f5;
  --warn: #df8e1d;
  --error: #d20f39;
  --fatal: #d20f39;
  --timestamp: #8c8fa1;
  --scrollbar: #bcc0cc;
  --shadow: rgba(0, 0, 0, 0.08);
}

@media (prefers-color-scheme: dark) {
  :root {
    /* Catppuccin Frappe (dark) */
    --bg: #303446;
    --surface: #292c3c;
    --card: #232634;
    --text: #c6d0f5;
    --text-muted: #838ba7;
    --border: #414559;
    --primary: #8caaee;
    --primary-hover: #99b4ee;
    --info: #8caaee;
    --warn: #e5c890;
    --error: #e78284;
    --fatal: #e78284;
    --timestamp: #737994;
    --scrollbar: #51576d;
    --shadow: rgba(0, 0, 0, 0.35);
  }
}

* { box-sizing: border-box; }
html, body { height: 100%; margin: 0; }
body {
  font-family: "Microsoft YaHei", "PingFang SC", "Helvetica Neue", Arial, sans-serif;
  font-size: 13px;
  line-height: 1.55;
  background: var(--bg);
  color: var(--text);
  display: flex;
  flex-direction: column;
}

/* 顶部标题栏卡片 */
header {
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  padding: 12px 16px;
  box-shadow: 0 2px 8px var(--shadow);
  position: sticky;
  top: 0;
  z-index: 10;
  flex-shrink: 0;
}
header h1 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--text);
  display: flex;
  align-items: center;
  gap: 8px;
}
header .badge {
  font-size: 11px;
  font-weight: 500;
  padding: 2px 6px;
  border-radius: 4px;
  background: var(--primary);
  color: #fff;
}

/* 主内容区 */
main {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  padding: 16px;
  gap: 12px;
}

/* 日志卡片 */
.log-card {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: 0 2px 10px var(--shadow);
  overflow: hidden;
  min-height: 0;
}
.log-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  font-size: 12px;
  color: var(--text-muted);
}
.log-header .count { font-variant-numeric: tabular-nums; }

/* 日志列表 */
#log-list {
  flex: 1;
  overflow-y: auto;
  padding: 12px 14px;
  font-family: "Cascadia Code", "JetBrains Mono", "Consolas", "Microsoft YaHei", monospace;
  font-size: 12.5px;
  line-height: 1.7;
}
#log-list::-webkit-scrollbar { width: 8px; }
#log-list::-webkit-scrollbar-track { background: transparent; }
#log-list::-webkit-scrollbar-thumb { background: var(--scrollbar); border-radius: 4px; }
#log-list::-webkit-scrollbar-thumb:hover { background: var(--text-muted); }

.log-entry {
  display: flex;
  gap: 10px;
  padding: 3px 0;
  border-bottom: 1px solid var(--border);
  animation: fadeIn 0.15s ease-out;
}
.log-entry:last-child { border-bottom: none; }
@keyframes fadeIn { from { opacity: 0; transform: translateY(2px); } to { opacity: 1; transform: none; } }

.log-time {
  flex: 0 0 auto;
  color: var(--timestamp);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.log-level {
  flex: 0 0 auto;
  min-width: 52px;
  text-align: center;
  font-size: 10.5px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.3px;
  border-radius: 3px;
  padding: 1px 6px;
}
.log-level.info  { background: color-mix(in srgb, var(--info) 15%, transparent); color: var(--info); }
.log-level.warn  { background: color-mix(in srgb, var(--warn) 15%, transparent); color: var(--warn); }
.log-level.error { background: color-mix(in srgb, var(--error) 15%, transparent); color: var(--error); }
.log-level.fatal { background: color-mix(in srgb, var(--fatal) 15%, transparent); color: var(--fatal); font-weight: 700; }

.log-msg {
  flex: 1;
  word-break: break-word;
  color: var(--text);
}

/* 底部提示卡片 */
footer {
  background: var(--surface);
  border-top: 1px solid var(--border);
  padding: 10px 16px;
  text-align: center;
  font-size: 11.5px;
  color: var(--text-muted);
  flex-shrink: 0;
}
footer kbd {
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 3px;
  padding: 1px 5px;
  font-family: inherit;
  font-size: 10.5px;
  color: var(--text);
}

/* 空状态 */
.empty-hint {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--text-muted);
  text-align: center;
  padding: 24px;
}
.empty-hint svg { width: 48px; height: 48px; margin-bottom: 10px; opacity: 0.5; }

/* 响应式 */
@media (max-width: 480px) {
  main { padding: 10px; gap: 8px; }
  header { padding: 10px 12px; }
  header h1 { font-size: 14px; }
  .log-header { padding: 8px 10px; font-size: 11px; }
  #log-list { padding: 10px; font-size: 12px; }
}
</style></head><body>
<header>
  <h1>VRTX 控制台 <span class="badge">实时</span></h1>
</header>
<main>
  <section class="log-card" aria-label="日志输出">
    <div class="log-header">
      <span>日志流</span>
      <span class="count" id="entry-count">0 条</span>
    </div>
    <div id="log-list" role="log" aria-live="polite" aria-atomic="false">
      <div class="empty-hint">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
          <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20M6.5 17H20M6.5 17H6.5" stroke-linecap="round" stroke-linejoin="round"/>
          <path d="M14 10a2 2 0 1 0 0 4 2 2 0 0 0 0-4Z" stroke-linecap="round" stroke-linejoin="round"/>
          <path d="M8 14h8" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
        <span>等待日志…</span>
      </div>
    </div>
  </section>
</main>
<footer>
  按 <kbd>Ctrl</kbd>+<kbd>L</kbd> 清空 · <span id="hint">VRTX 控制台 · 实时</span>
</footer>
<script>
(function() {
  var clsMap = {info:'info', warn:'warn', error:'error', fatal:'fatal'};
  var list = document.getElementById('log-list');
  var countEl = document.getElementById('entry-count');
  var entryCount = 0;

  var es = new EventSource('/stream');
  es.onmessage = function(ev) {
    var e = JSON.parse(ev.data);
    var div = document.createElement('div');
    div.className = 'log-entry';
    div.innerHTML =
      '<span class="log-time">' + e.time + '</span>' +
      '<span class="log-level ' + (clsMap[e.level]||'info') + '">' + (clsMap[e.level]||'info') + '</span>' +
      '<span class="log-msg">' + escapeHtml(e.msg) + '</span>';
    // 移除空状态提示
    var empty = list.querySelector('.empty-hint');
    if (empty) empty.remove();
    list.appendChild(div);
    entryCount++;
    countEl.textContent = entryCount + ' 条';
    // 自动滚动到底部（若用户已在底部附近）
    var nearBottom = list.scrollTop + list.clientHeight >= list.scrollHeight - 50;
    if (nearBottom) {
      list.scrollTop = list.scrollHeight;
    }
  };
  es.onerror = function() { es.close(); };

  // Ctrl+L 清空
  document.addEventListener('keydown', function(e) {
    if (e.ctrlKey && e.key === 'l') {
      e.preventDefault();
      list.innerHTML = '<div class="empty-hint"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20M6.5 17H20M6.5 17H6.5" stroke-linecap="round" stroke-linejoin="round"/><path d="M14 10a2 2 0 1 0 0 4 2 2 0 0 0 0-4Z" stroke-linecap="round" stroke-linejoin="round"/><path d="M8 14h8" stroke-linecap="round" stroke-linejoin="round"/></svg><span>已清空，等待日志…</span></div>';
      entryCount = 0;
      countEl.textContent = '0 条';
    }
  });

  function escapeHtml(str) {
    return String(str).replace(/[&<>"']/g, function(c) {
      return {'&':'&','<':'<','>':'>','"':'"',"'":'''}[c];
    });
  }
})();
</script>
</body></html>`
}
