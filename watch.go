package main

import (
	"os"
	"path/filepath"
	"time"
)

func startWatch(outputDir string, interval time.Duration, sigCh <-chan os.Signal, watchBookmarks, watchShortcuts bool) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var bookmarkModTimes map[string]time.Time
	if watchBookmarks {
		bookmarkModTimes = make(map[string]time.Time)
		for _, p := range getBookmarkPaths() {
			if fi, err := os.Stat(p); err == nil {
				bookmarkModTimes[p] = fi.ModTime()
			}
		}
	}

	var shortcutModTimes map[string]time.Time
	if watchShortcuts {
		shortcutModTimes = make(map[string]time.Time)
		for _, dir := range getShortcutSrcDirs() {
			if fi, err := os.Stat(dir); err == nil {
				shortcutModTimes[dir] = fi.ModTime()
			}
		}
	}

	logInfo("监控已启动，轮询间隔 %v（按 Ctrl+C 停止）", interval)

	for {
		select {
		case <-ticker.C:
			if watchBookmarks {
				changed := false
				for _, p := range getBookmarkPaths() {
					fi, err := os.Stat(p)
					if err != nil {
						continue
					}
					prev, ok := bookmarkModTimes[p]
					if !ok || fi.ModTime().After(prev) {
						bookmarkModTimes[p] = fi.ModTime()
						changed = true
					}
				}
				if changed {
					logInfo("检测到书签变更，正在重建...")
					os.RemoveAll(filepath.Join(outputDir, "Bookmarks"))
					extractBookmarks(outputDir)
					logInfo("书签重建完成！")
				}
			}

			if watchShortcuts {
				changed := false
				for _, dir := range getShortcutSrcDirs() {
					fi, err := os.Stat(dir)
					if err != nil {
						continue
					}
					prev, ok := shortcutModTimes[dir]
					if !ok || fi.ModTime().After(prev) {
						shortcutModTimes[dir] = fi.ModTime()
						changed = true
					}
				}
				if changed {
					logInfo("检测到快捷方式变更，正在重建...")
					os.RemoveAll(filepath.Join(outputDir, "Shortcuts"))
					extractShortcuts(outputDir)
					logInfo("快捷方式重建完成！")
				}
			}

		case <-sigCh:
			logInfo("监控已停止，文件保留在 %s", outputDir)
			return
		}
	}
}
