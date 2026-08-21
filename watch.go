package main

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// startWatch 以轮询方式监控文件变更，直到 ctx 被取消。
// 监控架构：time.Ticker 周期性触发 → 对比文件 ModTime 判断是否变化 → 增量重建。
// 书签和各快捷方式来源的检测相互独立，各自判断各自重建。
func startWatch(ctx context.Context, outputDir string, interval time.Duration, watchBookmarks, watchSoftware, watchSystem, watchDrives, watchRecent, watchOffice bool) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 首次运行记录基线时间戳，后续轮询与之比较来判断是否发生变化
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
	var lastDrives []string
	if watchSoftware || watchRecent || watchOffice {
		shortcutModTimes = make(map[string]time.Time)
		for _, dir := range getShortcutSrcDirs(watchSoftware, watchRecent, watchOffice) {
			if fi, err := os.Stat(dir); err == nil {
				shortcutModTimes[dir] = fi.ModTime()
			}
		}
	}
	if watchDrives {
		lastDrives = getAvailableDrives()
	}

	logInfo("监控已启动，轮询间隔 %v（按 Ctrl+C 停止）", interval)

	for {
		select {
		case <-ticker.C:
			// 书签变更检测：比较每个书签文件的 mtime
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

			// 快捷方式变更检测：比较源目录的 mtime 以及可用盘符变化
			if watchSoftware || watchRecent || watchOffice || watchDrives {
				changed := false
				if watchSoftware || watchRecent || watchOffice {
					for _, dir := range getShortcutSrcDirs(watchSoftware, watchRecent, watchOffice) {
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
				}

				if watchDrives {
					drives := getAvailableDrives()
					if !driveListsEqual(drives, lastDrives) {
						lastDrives = drives
						changed = true
					}
				}

				if changed {
					logInfo("检测到快捷方式变更，正在重建...")
					os.RemoveAll(filepath.Join(outputDir, "Shortcuts"))
					extractShortcuts(outputDir, watchSoftware, watchSystem, watchDrives, watchRecent, watchOffice)
					logInfo("快捷方式重建完成！")
				}
			}

		case <-ctx.Done():
			logInfo("监控已停止，文件保留在 %s", outputDir)
			return
		}
	}
}

// driveListsEqual 比较两个盘符列表是否相同。
// 不做排序对比，因为盘符顺序本身具有稳定性（C 永远在 D 前面）。
func driveListsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
