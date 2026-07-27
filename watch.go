package main

import (
	"os"
	"path/filepath"
	"time"
)

func startWatch(outputDir string, interval time.Duration, sigCh <-chan os.Signal) {
	paths := getBookmarkPaths()

	modTimes := make(map[string]time.Time)
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil {
			modTimes[p] = fi.ModTime()
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logInfo("监控已启动，轮询间隔 %v（按 Ctrl+C 停止）", interval)

	for {
		select {
		case <-ticker.C:
			changed := false
			for _, p := range paths {
				fi, err := os.Stat(p)
				if err != nil {
					continue
				}
				prev, ok := modTimes[p]
				if !ok || fi.ModTime().After(prev) {
					modTimes[p] = fi.ModTime()
					changed = true
				}
			}
			if changed {
				logInfo("检测到书签变更，正在重建...")
				bookmarkDir := filepath.Join(outputDir, "Bookmarks")
				os.RemoveAll(bookmarkDir)
				extractBookmarks(outputDir)
				logInfo("重建完成！")
			}
		case <-sigCh:
			logInfo("监控已停止，快捷方式保留在 %s", outputDir)
			return
		}
	}
}
