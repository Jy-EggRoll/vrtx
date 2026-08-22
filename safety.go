package main

import (
	"fmt"
	"os"
	"strings"
)

// managedEntries 是 VRTX 输出目录的顶层条目白名单。
// .vrtx-owned 是旧版本写入的所有权标记：写入逻辑已删除，
// 白名单仍保留该项，让存量标记在下次清场时被合法顺带清理。
var managedEntries = map[string]bool{
	"Bookmarks":   true,
	"Shortcuts":   true,
	"VSCode":      true,
	".vrtx-owned": true,
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

// removeOwnedDir 带所有权校验的目录删除，仅限根级操作：
// 根通过白名单即确立整棵树归 VRTX 所有，其下子目录无需再验。
func removeOwnedDir(dir string) error {
	if err := ensureOwnedDir(dir); err != nil {
		return fmt.Errorf("拒绝删除 %s：%w", dir, err)
	}
	return os.RemoveAll(dir)
}
