package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// markerFileName 是输出目录的所有权标记；每次初始化/迁移后写入，
// 供未来版本进一步收紧校验（当前规则不依赖它，兼容老版本残留目录）
const markerFileName = ".vrtx-owned"

// managedEntries 是 VRTX 输出目录的顶层条目白名单：
// 删除准入与写入准入共用同一谓词——目录里只有这些东西才允许碰
var managedEntries = map[string]bool{
	"Bookmarks":    true,
	"Shortcuts":    true,
	"VSCode":       true,
	markerFileName: true,
}

// ensureOwnedDir 是删/写两侧共用的底线校验：
// 目录不存在 / 为空 / 顶层仅含 VRTX 管理条目，三者之一放行；
// 否则返回错误并列出外来条目。C:\、用户文档等一律进不来。
func ensureOwnedDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 不存在视为安全（写入侧将创建）
		}
		return fmt.Errorf("读取目录失败: %w", err)
	}
	var foreign []string
	for _, e := range entries {
		if !managedEntries[e.Name()] {
			foreign = append(foreign, e.Name())
		}
	}
	if len(foreign) == 0 {
		return nil
	}
	const maxShow = 8
	if len(foreign) > maxShow {
		foreign = append(foreign[:maxShow], fmt.Sprintf("…等 %d 项", len(foreign)))
	}
	return fmt.Errorf("该目录包含非 VRTX 内容：%s", strings.Join(foreign, "、"))
}

// removeOwnedDir 带所有权校验的目录删除：非 VRTX 目录拒绝清空
func removeOwnedDir(dir string) error {
	if err := ensureOwnedDir(dir); err != nil {
		return fmt.Errorf("拒绝删除 %s：%w", dir, err)
	}
	return os.RemoveAll(dir)
}

// markOwned 在输出目录写入所有权标记（尽力而为，失败不影响主流程）
func markOwned(dir string) {
	_ = os.WriteFile(filepath.Join(dir, markerFileName), []byte("此目录由 VRTX 管理，存放自动生成的快捷方式\n"), 0644)
}
