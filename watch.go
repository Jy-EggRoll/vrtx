package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// startWatch 以轮询方式监控文件变更，直到 ctx 被取消。
// 监控架构：time.Ticker 周期性触发 → 对比文件 ModTime 判断是否变化 → 增量重建。
// 书签和各快捷方式来源的检测相互独立，各自判断各自重建；
// 触发重建时精确报告是哪个文件/目录/盘符发生了变化。
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
			// 书签变更检测：比较每个书签文件的 mtime，收集具体变更文件
			if watchBookmarks {
				var changedFiles []string
				for _, p := range getBookmarkPaths() {
					fi, err := os.Stat(p)
					if err != nil {
						continue
					}
					prev, ok := bookmarkModTimes[p]
					if !ok || fi.ModTime().After(prev) {
						bookmarkModTimes[p] = fi.ModTime()
						changedFiles = append(changedFiles, p)
					}
				}
				if len(changedFiles) > 0 {
					logInfo("书签文件已变更：%s", strings.Join(changedFiles, "、"))
					logInfo("正在重建书签...")
					os.RemoveAll(filepath.Join(outputDir, "Bookmarks"))
					n := extractBookmarks(outputDir)
					logInfo("书签重建完成：共 %d 个 .url", n)
				}
			}

			// 快捷方式变更检测：比较源目录的 mtime 以及可用盘符变化
			if watchSoftware || watchRecent || watchOffice || watchDrives {
				var changedDirs []string
				if watchSoftware || watchRecent || watchOffice {
					for _, dir := range getShortcutSrcDirs(watchSoftware, watchRecent, watchOffice) {
						fi, err := os.Stat(dir)
						if err != nil {
							continue
						}
						prev, ok := shortcutModTimes[dir]
						if !ok || fi.ModTime().After(prev) {
							shortcutModTimes[dir] = fi.ModTime()
							changedDirs = append(changedDirs, dir)
						}
					}
				}

				var addedDrives, removedDrives []string
				if watchDrives {
					drives := getAvailableDrives()
					addedDrives, removedDrives = diffDrives(lastDrives, drives)
					if len(addedDrives)+len(removedDrives) > 0 {
						lastDrives = drives
					}
				}

				if len(changedDirs) > 0 || len(addedDrives) > 0 || len(removedDrives) > 0 {
					if len(changedDirs) > 0 {
						logInfo("快捷方式目录已变更：%s", strings.Join(changedDirs, "、"))
					}
					if len(addedDrives) > 0 || len(removedDrives) > 0 {
						logInfo("盘符变化：新增 %s，移除 %s", driveDisplay(addedDrives), driveDisplay(removedDrives))
					}
					logInfo("正在重建快捷方式...")
					os.RemoveAll(filepath.Join(outputDir, "Shortcuts"))
					extractShortcuts(outputDir, watchSoftware, watchSystem, watchDrives, watchRecent, watchOffice)
					logInfo("快捷方式重建完成")
				}
			}

		case <-ctx.Done():
			logInfo("监控已停止，文件保留在 %s", outputDir)
			return
		}
	}
}

// diffDrives 求盘符列表的新增与移除差集。
// 盘符顺序本身稳定（C 永远在 D 前），无需排序即可得到确定性结果。
func diffDrives(oldDrives, newDrives []string) (added, removed []string) {
	oldSet := make(map[string]struct{}, len(oldDrives))
	for _, d := range oldDrives {
		oldSet[d] = struct{}{}
	}
	newSet := make(map[string]struct{}, len(newDrives))
	for _, d := range newDrives {
		if _, ok := oldSet[d]; !ok {
			added = append(added, d)
		}
		newSet[d] = struct{}{}
	}
	for _, d := range oldDrives {
		if _, ok := newSet[d]; !ok {
			removed = append(removed, d)
		}
	}
	return added, removed
}

// driveDisplay 将盘符字母列表格式化为可读文本（如 "E:\"、"无"）
func driveDisplay(drives []string) string {
	if len(drives) == 0 {
		return "无"
	}
	out := make([]string, len(drives))
	for i, d := range drives {
		out[i] = d + `:\`
	}
	return strings.Join(out, "、")
}
