package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BookmarkNode 对应 Chrome/Edge 书签 JSON 中的递归节点结构。
//   - type="folder" 的节点包含 Children，无 URL
//   - type="url" 的节点包含 URL，无 Children
type BookmarkNode struct {
	Children []BookmarkNode `json:"children,omitempty"`
	Name     string         `json:"name,omitempty"`
	Type     string         `json:"type,omitempty"`
	URL      string         `json:"url,omitempty"`
}

type BookmarkRoot struct {
	Roots map[string]BookmarkNode `json:"roots"`
}

type BookmarkInfo struct {
	Name string
	URL  string
	Path string
}

// getBookmarkPaths 返回 Chrome/Edge 书签文件的可能路径。
// 路径硬编码为 Windows 特有路径（仅在 Windows 下有效）。
// 注意：仅读取 Default profile，不考虑多 profile 场景。
func getBookmarkPaths() []string {
	homeDir, _ := os.UserHomeDir()
	return []string{
		filepath.Join(homeDir, "AppData", "Local", "Microsoft", "Edge", "User Data", "Default", "Bookmarks"),
		filepath.Join(homeDir, "AppData", "Local", "Google", "Chrome", "User Data", "Default", "Bookmarks"),
	}
}

// extractBookmarks 遍历每个书签文件，为每个存在的文件启动一个 goroutine 并发处理。
// 并发粒度是"每个浏览器文件"而非"每条书签"，避免 goroutine 过多。
func extractBookmarks(outputDir string) {
	bookmarkDir := filepath.Join(outputDir, "Bookmarks")
	os.MkdirAll(bookmarkDir, 0755)

	var tasks []func()
	for _, path := range getBookmarkPaths() {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		p := path
		tasks = append(tasks, func() { processBookmarkFile(p, bookmarkDir) })
	}
	runConcurrent(tasks...)
}

func processBookmarkFile(path, bookmarkDir string) {
	data, err := os.ReadFile(path)
	if err != nil {
		logError("读取书签文件失败: %v", err)
		return
	}

	var root BookmarkRoot
	if err := json.Unmarshal(data, &root); err != nil {
		logError("解析书签 JSON 失败: %v", err)
		return
	}

	var bookmarks []BookmarkInfo
	for _, node := range root.Roots {
		collectBookmarkInfo(&node, "", &bookmarks)
	}

	var tasks []func()
	for _, bm := range bookmarks {
		b := bm
		tasks = append(tasks, func() { createURLFile(bookmarkDir, b) })
	}
	runConcurrent(tasks...)
}

// collectBookmarkInfo 递归遍历 Chrome/Edge 书签 JSON 树。
// 遍历规则：
//   - type="folder" 节点：将文件夹名追加到父路径前缀（用 "-" 拼接），继续递归 children
//   - type="url" 节点：收集到 bookmarks 切片中，附带当前路径前缀
func collectBookmarkInfo(node *BookmarkNode, parentPath string, bookmarks *[]BookmarkInfo) {
	if node.Type == "url" && node.URL != "" {
		*bookmarks = append(*bookmarks, BookmarkInfo{
			Name: node.Name,
			URL:  node.URL,
			Path: parentPath,
		})
	}

	for i := range node.Children {
		child := &node.Children[i]
		newPath := parentPath

		if child.Type == "folder" {
			if parentPath == "" {
				newPath = sanitizeFileName(child.Name)
			} else {
				newPath = parentPath + "-" + sanitizeFileName(child.Name)
			}
		}

		collectBookmarkInfo(child, newPath, bookmarks)
	}
}

func createURLFile(bookmarkDir string, bm BookmarkInfo) {
	name := sanitizeFileName(bm.Name)
	if name == "" {
		name = "unnamed"
	}

	host := extractHost(bm.URL)

	var filename string
	if bm.Path != "" {
		filename = fmt.Sprintf("%s-%s-%s.url", name, bm.Path, host)
	} else {
		filename = fmt.Sprintf("%s-%s.url", name, host)
	}

	filename = sanitizeFileName(filename)

	path := filepath.Join(bookmarkDir, filename)
	path = getUniquePath(path)

	content := fmt.Sprintf("[InternetShortcut]\nURL=%s\n", bm.URL)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		logWarn("创建快捷方式失败: %v", err)
	}
}

func extractHost(url string) string {
	host := url
	if idx := strings.Index(host, "://"); idx != -1 {
		host = host[idx+3:]
	}
	if idx := strings.Index(host, "/"); idx != -1 {
		host = host[:idx]
	}
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	host = strings.TrimPrefix(host, "www.")
	return host
}

func sanitizeFileName(name string) string {
	invalid := []string{"<", ">", ":", "\"", "/", "\\", "|", "?", "*"}
	for _, c := range invalid {
		name = strings.ReplaceAll(name, c, "_")
	}
	return strings.Trim(strings.TrimSpace(name), ".")
}

// getUniquePath 在路径已存在时追加 _1, _2, _3... 后缀以保持唯一性。
// 假设磁盘空间足够且没有无限多的同名文件，理论上不会触发无限循环。
func getUniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	name := strings.TrimSuffix(filepath.Base(path), ext)
	for i := 1; ; i++ {
		p := filepath.Join(dir, fmt.Sprintf("%s_%d%s", name, i, ext))
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
	}
}
