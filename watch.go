//go:build windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// watchState 汇集监控循环的可变内部状态；输出目录热切换时整体重建。
type watchState struct {
	interval         time.Duration
	outDir           string
	ticker           *time.Ticker
	cfgMtime         time.Time // 上次见到的 vrtx.json mtime，用于检测外部手改
	prev             ExtractConfig
	bookmarkModTimes map[string]time.Time
	shortcutModTimes map[string]time.Time
	vscodeModTimes   map[string]time.Time
	lastDrives       []string
}

func newWatchState(cfg *Config) *watchState {
	st := &watchState{
		interval: cfg.Interval(),
		outDir:   cfg.OutputPath(),
		prev:     cfg.Extract,
	}
	st.ticker = time.NewTicker(st.interval)
	if fi, err := os.Stat(configPath); err == nil {
		st.cfgMtime = fi.ModTime()
	}
	st.resetBaselines(cfg)
	return st
}

func anyShortcutSrc(e ExtractConfig) bool {
	return e.Software || e.Recent || e.Office
}

func shortcutFlagsChanged(a, b ExtractConfig) bool {
	return a.Software != b.Software || a.System != b.System || a.Drives != b.Drives ||
		a.Recent != b.Recent || a.Office != b.Office
}

// resetBaselines 按当前配置登记各监控源的 mtime / 盘符基线；
// 停用的类别基线清空（输出文件按约定保留）
func (st *watchState) resetBaselines(cfg *Config) {
	e := cfg.Extract
	st.bookmarkModTimes = nil
	if e.Bookmarks {
		st.bookmarkModTimes = statModTimes(getBookmarkPaths())
	}
	st.shortcutModTimes = nil
	if anyShortcutSrc(e) {
		st.shortcutModTimes = statModTimes(getShortcutSrcDirs(e.Software, e.Recent, e.Office))
	}
	st.lastDrives = nil
	if e.Drives {
		st.lastDrives = getAvailableDrives()
	}
	st.vscodeModTimes = nil
	if e.VSCode {
		st.vscodeModTimes = statModTimes(getVSCodeDBPaths())
	}
}

// statModTimes 批量登记路径的 mtime 基线（不存在的路径跳过）
func statModTimes(paths []string) map[string]time.Time {
	m := make(map[string]time.Time, len(paths))
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil {
			m[p] = fi.ModTime()
		}
	}
	return m
}

// syncConfig 对比配置快照，应用可热更的变更：
// 外部手改 vrtx.json 热载、轮询间隔调整、输出目录热切换、类别启停。
func syncConfig(st *watchState) {
	// 外部手改配置文件热载：自身保存造成的 mtime 变化因内容一致而静默跳过
	if fi, err := os.Stat(configPath); err == nil && !fi.ModTime().Equal(st.cfgMtime) {
		st.cfgMtime = fi.ModTime()
		if nc := loadConfigFile(configPath); !reflect.DeepEqual(nc, current()) {
			// 写入侧底线：新目录不合规就拒绝应用，继续用旧配置跑
			if err := ensureOwnedDir(nc.OutputPath()); err != nil {
				logError("配置文件中的输出目录不可用，保持当前设置：%v", err)
			} else {
				cfgPtr.Store(nc)
				logInfo("已从配置文件重新加载设置")
			}
		}
	}

	cfg := current()
	e := cfg.Extract

	// 轮询间隔变更：重置 ticker 即刻生效
	if d := cfg.Interval(); d != st.interval {
		st.interval = d
		st.ticker.Reset(d)
		logInfo("轮询间隔已调整为 %v", d)
	}

	// 输出目录热切换：旧目录凭所有权删除（删不动则原样保留），新目录准入后迁移
	if np := cfg.OutputPath(); np != st.outDir {
		old := st.outDir
		st.outDir = np
		logInfo("输出目录切换：%s → %s，正在迁移...", old, np)
		if err := removeOwnedDir(old); err != nil {
			logError("旧输出目录未删除（原样保留）：%v", err)
		}
		if err := os.MkdirAll(np, 0755); err != nil {
			logError("创建新输出目录失败：%v", err)
			return
		}
		markOwned(np)
		runFullExtract(np)
		st.resetBaselines(cfg)
		logInfo("输出目录迁移完成")
		st.prev = e
		return
	}

	// 类别启停：启用→清掉对应子目录重建（避免与既有文件叠加产生 _1 副本）；
	// 停用→跳过监控但保留已生成输出
	if e.Bookmarks != st.prev.Bookmarks {
		if e.Bookmarks && rebuildSubdir(st.outDir, "Bookmarks") {
			logInfo("书签提取已启用，正在重建...")
			extractBookmarks(st.outDir)
		} else if !e.Bookmarks {
			logInfo("书签提取已停用（已有输出保留）")
		}
		st.bookmarkModTimes = nil
		if e.Bookmarks {
			st.bookmarkModTimes = statModTimes(getBookmarkPaths())
		}
	}

	if shortcutFlagsChanged(e, st.prev) {
		on := e.Software || e.System || e.Drives || e.Recent || e.Office
		if on && rebuildSubdir(st.outDir, "Shortcuts") {
			logInfo("快捷方式提取配置已变更，正在重建...")
			extractShortcuts(st.outDir, e.Software, e.System, e.Drives, e.Recent, e.Office)
		} else if !on {
			logInfo("快捷方式提取已停用（已有输出保留）")
		}
		st.shortcutModTimes = nil
		st.lastDrives = nil
		if anyShortcutSrc(e) {
			st.shortcutModTimes = statModTimes(getShortcutSrcDirs(e.Software, e.Recent, e.Office))
		}
		if e.Drives {
			st.lastDrives = getAvailableDrives()
		}
	}

	if e.VSCode != st.prev.VSCode {
		if e.VSCode && rebuildSubdir(st.outDir, "VSCode") {
			logInfo("VS Code 提取已启用，正在重建...")
			extractVSCodeShortcuts(st.outDir)
		} else if !e.VSCode {
			logInfo("VS Code 提取已停用（已有输出保留）")
		}
		st.vscodeModTimes = nil
		if e.VSCode {
			st.vscodeModTimes = statModTimes(getVSCodeDBPaths())
		}
	}

	st.prev = e
}

// rebuildSubdir 清空输出目录下某个管理子目录；守卫校验失败时拒绝并返回 false，
// 调用方应放弃对该子目录的重建
func rebuildSubdir(outDir, name string) bool {
	sub := filepath.Join(outDir, name)
	if err := removeOwnedDir(sub); err != nil {
		logError("清理 %s 失败，跳过重建：%v", sub, err)
		return false
	}
	os.MkdirAll(sub, 0755)
	return true
}

// startWatch 监控主循环：每个 tick 先同步配置（热生效），再按当前配置做变更检测。
func startWatch(ctx context.Context) {
	cfg := current()
	st := newWatchState(cfg)
	defer st.ticker.Stop()

	logInfo("监控已启动，轮询间隔 %v", st.interval)
	if !cfg.Watch {
		logInfo("监控模式当前为关闭状态，可在网页设置中开启")
	}

	for {
		select {
		case <-st.ticker.C:
			syncConfig(st)

			cfg = current()
			if !cfg.Watch {
				continue // 监控总开关关闭：空转等待重新开启
			}
			e := cfg.Extract

			// 书签变更检测
			if e.Bookmarks && st.bookmarkModTimes != nil {
				var changedFiles []string
				for _, p := range getBookmarkPaths() {
					fi, err := os.Stat(p)
					if err != nil {
						continue
					}
					prev, ok := st.bookmarkModTimes[p]
					if !ok || fi.ModTime().After(prev) {
						st.bookmarkModTimes[p] = fi.ModTime()
						changedFiles = append(changedFiles, p)
					}
				}
				if len(changedFiles) > 0 && rebuildSubdir(st.outDir, "Bookmarks") {
					logInfo("书签文件已变更：%s", strings.Join(changedFiles, "、"))
					n := extractBookmarks(st.outDir)
					logInfo("书签重建完成：共 %d 个 .url", n)
				}
			}

			// 快捷方式变更检测（源目录 mtime + 盘符差集）
			if (anyShortcutSrc(e) && st.shortcutModTimes != nil) || e.Drives {
				var changedDirs []string
				if anyShortcutSrc(e) && st.shortcutModTimes != nil {
					for _, dir := range getShortcutSrcDirs(e.Software, e.Recent, e.Office) {
						fi, err := os.Stat(dir)
						if err != nil {
							continue
						}
						prev, ok := st.shortcutModTimes[dir]
						if !ok || fi.ModTime().After(prev) {
							st.shortcutModTimes[dir] = fi.ModTime()
							changedDirs = append(changedDirs, dir)
						}
					}
				}
				var addedDrives, removedDrives []string
				if e.Drives {
					drives := getAvailableDrives()
					addedDrives, removedDrives = diffDrives(st.lastDrives, drives)
					if len(addedDrives)+len(removedDrives) > 0 {
						st.lastDrives = drives
					}
				}
				if len(changedDirs)+len(addedDrives)+len(removedDrives) > 0 &&
					rebuildSubdir(st.outDir, "Shortcuts") {
					if len(changedDirs) > 0 {
						logInfo("快捷方式目录已变更：%s", strings.Join(changedDirs, "、"))
					}
					if len(addedDrives) > 0 || len(removedDrives) > 0 {
						logInfo("盘符变化：新增 %s，移除 %s",
							driveDisplay(addedDrives), driveDisplay(removedDrives))
					}
					extractShortcuts(st.outDir, e.Software, e.System, e.Drives, e.Recent, e.Office)
					logInfo("快捷方式重建完成")
				}
			}

			// VS Code 历史记录检测
			if e.VSCode && st.vscodeModTimes != nil {
				var changedDBs []string
				for _, p := range getVSCodeDBPaths() {
					fi, err := os.Stat(p)
					if err != nil {
						continue
					}
					prev, ok := st.vscodeModTimes[p]
					if !ok || fi.ModTime().After(prev) {
						st.vscodeModTimes[p] = fi.ModTime()
						changedDBs = append(changedDBs, p)
					}
				}
				if len(changedDBs) > 0 && rebuildSubdir(st.outDir, "VSCode") {
					logInfo("VS Code 历史记录已变更：%s", strings.Join(changedDBs, "、"))
					n := extractVSCodeShortcuts(st.outDir)
					logInfo("VS Code 快捷方式重建完成：新建 %d 个", n)
				}
			}

		case <-ctx.Done():
			logInfo("监控已停止，文件保留在 %s", st.outDir)
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

// runFullExtract 按当前配置执行全部启用的提取任务（首次启动/目录迁移共用）
func runFullExtract(dir string) {
	e := current().Extract
	if e.Bookmarks {
		logInfo("正在提取书签...")
		extractBookmarks(dir)
	}
	if e.Software || e.System || e.Drives || e.Recent || e.Office {
		logInfo("正在提取快捷方式...")
		extractShortcuts(dir, e.Software, e.System, e.Drives, e.Recent, e.Office)
	}
	if e.VSCode {
		logInfo("正在提取 VS Code 连接...")
		extractVSCodeShortcuts(dir)
	}
}

// cleanAndRebuild 清空当前输出目录并按当前配置立即重建。
// 目录含非 VRTX 内容时拒绝清理（底线守卫），不做半吊子重建。
func cleanAndRebuild() {
	dir := current().OutputPath()
	logInfo("正在清理输出目录：%s", dir)
	if err := removeOwnedDir(dir); err != nil {
		logError("清理失败，已中止：%v", err)
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		logError("重建输出目录失败：%v", err)
		return
	}
	markOwned(dir)
	runFullExtract(dir)
	logInfo("清理并重建完成")
}
