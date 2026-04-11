package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// BookmarkNode 书签 JSON 节点
type BookmarkNode struct {
	Children []BookmarkNode `json:"children,omitempty"`
	Name     string         `json:"name,omitempty"`
	Type     string         `json:"type,omitempty"`
	URL      string         `json:"url,omitempty"`
}

// BookmarkRoot 书签文件根
type BookmarkRoot struct {
	Roots map[string]BookmarkNode `json:"roots"`
}

// BookmarkInfo 携带路径信息的书签
type BookmarkInfo struct {
	Name     string // 书签名称
	URL      string // 网址
	Path     string // 收藏夹路径，如 "北邮-学习"
	FullName string // 完整文件名（缓存）
}

// extractBookmarks 提取书签并生成 .url 文件
func extractBookmarks(outputDir string) {
	bookmarkDir := filepath.Join(outputDir, "Bookmarks")
	os.MkdirAll(bookmarkDir, 0755)

	homeDir, _ := os.UserHomeDir()
	paths := []string{
		filepath.Join(homeDir, "AppData", "Local", "Microsoft", "Edge", "User Data", "Default", "Bookmarks"),
		filepath.Join(homeDir, "AppData", "Local", "Google", "Chrome", "User Data", "Default", "Bookmarks"),
	}

	var wg sync.WaitGroup
	for _, path := range paths {
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

// processBookmarkFile 处理单个书签文件
func processBookmarkFile(path, bookmarkDir string) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Read bookmark file failed: %v", err)
		return
	}

	var root BookmarkRoot
	if err := json.Unmarshal(data, &root); err != nil {
		log.Printf("Parse bookmark failed: %v", err)
		return
	}

	// 收集书签（携带路径信息）
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

// collectBookmarkInfo 递归收集书签，记录文件夹路径
func collectBookmarkInfo(node *BookmarkNode, parentPath string, bookmarks *[]BookmarkInfo) {
	if node.Type == "url" && node.URL != "" {
		*bookmarks = append(*bookmarks, BookmarkInfo{
			Name: node.Name,
			URL:  node.URL,
			Path: parentPath,
		})
	}

	// 递归处理所有子节点
	for i := range node.Children {
		child := &node.Children[i]
		newPath := parentPath

		// 如果子节点是文件夹，更新路径
		if child.Type == "folder" {
			if parentPath == "" {
				newPath = sanitizeFileName(child.Name)
			} else {
				newPath = parentPath + "-" + sanitizeFileName(child.Name)
			}
		}

		// 递归处理子节点（传递更新后的路径）
		collectBookmarkInfo(child, newPath, bookmarks)
	}
}

// createURLFile 创建 .url 快捷方式
// 文件名格式：书签名称-收藏夹路径-网址.url
func createURLFile(bookmarkDir string, bm BookmarkInfo) {
	// 构造文件名：书名-路径-网址
	name := sanitizeFileName(bm.Name)
	if name == "" {
		name = "unnamed"
	}

	// 提取域名作为网址部分
	host := extractHost(bm.URL)

	// 完整文件名：书名-路径-域名.url
	var filename string
	if bm.Path != "" {
		filename = fmt.Sprintf("%s-%s-%s.url", name, bm.Path, host)
	} else {
		filename = fmt.Sprintf("%s-%s.url", name, host)
	}

	// 清理文件名（再次确保合法）
	filename = sanitizeFileName(filename)

	path := filepath.Join(bookmarkDir, filename)
	path = getUniquePath(path) // 使用通用的唯一路径函数

	content := fmt.Sprintf("[InternetShortcut]\nURL=%s\n", bm.URL)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		log.Printf("Create URL file failed: %v", err)
	}
}

// extractHost 从 URL 提取域名
func extractHost(url string) string {
	// 简单提取：去掉协议和路径
	host := url
	// 去掉协议
	if idx := strings.Index(host, "://"); idx != -1 {
		host = host[idx+3:]
	}
	// 去掉路径
	if idx := strings.Index(host, "/"); idx != -1 {
		host = host[:idx]
	}
	// 去掉端口
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	// 去掉 www.
	host = strings.TrimPrefix(host, "www.")
	return host
}

// sanitizeFileName 清理非法字符
func sanitizeFileName(name string) string {
	invalid := []string{"<", ">", ":", "\"", "/", "\\", "|", "?", "*"}
	for _, c := range invalid {
		name = strings.ReplaceAll(name, c, "_")
	}
	return strings.Trim(strings.TrimSpace(name), ".")
}

// getUniquePath 如果文件已存在，生成唯一路径
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
