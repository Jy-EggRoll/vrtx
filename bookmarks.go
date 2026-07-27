package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

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

func getBookmarkPaths() []string {
	homeDir, _ := os.UserHomeDir()
	return []string{
		filepath.Join(homeDir, "AppData", "Local", "Microsoft", "Edge", "User Data", "Default", "Bookmarks"),
		filepath.Join(homeDir, "AppData", "Local", "Google", "Chrome", "User Data", "Default", "Bookmarks"),
	}
}

func extractBookmarks(outputDir string) {
	bookmarkDir := filepath.Join(outputDir, "Bookmarks")
	os.MkdirAll(bookmarkDir, 0755)

	var wg sync.WaitGroup
	for _, path := range getBookmarkPaths() {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			processBookmarkFile(p, bookmarkDir)
		}(path)
	}
	wg.Wait()
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

	var wg sync.WaitGroup
	for _, bm := range bookmarks {
		wg.Add(1)
		go func(b BookmarkInfo) {
			defer wg.Done()
			createURLFile(bookmarkDir, b)
		}(bm)
	}
	wg.Wait()
}

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
