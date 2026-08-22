//go:build windows

package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// vscodeEntry 是 history.recentlyOpenedPathsList 中的一条打开记录。
// 注意：工作区文件（.code-workspace）可能出现在 FolderURI 字段而非 FileURI。
type vscodeEntry struct {
	FolderURI string `json:"folderUri"`
	FileURI   string `json:"fileUri"`
	Label     string `json:"label"`
}

type vscodeHistory struct {
	Entries []vscodeEntry `json:"entries"`
}

// vscodeKind 标记条目类型。文件类条目仅用于统计，不生成快捷方式。
type vscodeKind int

const (
	vscodeFolder vscodeKind = iota
	vscodeFile
	vscodeWorkspace
)

// getVSCodeDBPaths 返回候选的 state.vscdb 路径（稳定版 + Insiders）。
// 供提取与监控两处共用，保证监控的文件集合与实际读取的一致。
func getVSCodeDBPaths() []string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return nil
	}
	return []string{
		filepath.Join(appData, "Code", "User", "globalStorage", "state.vscdb"),
		filepath.Join(appData, "Code - Insiders", "User", "globalStorage", "state.vscdb"),
	}
}

// findCodeExe 查找 VS Code 可执行文件：常见安装位置优先，再兜底 PATH 中的 Code.exe
func findCodeExe() string {
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft VS Code", "Code.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft VS Code", "Code.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Microsoft VS Code", "Code.exe"),
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("Code.exe"); err == nil {
		return p
	}
	return ""
}

// extractVSCodeShortcuts 从 state.vscdb 的最近打开记录生成快捷方式（本地/远程的文件夹与工作区，
// 单个文件的打开记录不生成），返回新建数量。
func extractVSCodeShortcuts(outputDir string) int {
	codeExe := findCodeExe()
	if codeExe == "" {
		logWarn("未找到 VS Code 可执行文件，跳过 VS Code 快捷方式生成")
		return 0
	}

	var entries []vscodeEntry
	foundDB := false
	for _, dbPath := range getVSCodeDBPaths() {
		if _, err := os.Stat(dbPath); err != nil {
			continue
		}
		foundDB = true
		es, err := loadRecentEntries(dbPath)
		if err != nil {
			logError("读取 %s 失败: %v", dbPath, err)
			continue
		}
		entries = append(entries, es...)
	}
	if !foundDB {
		logDebug("未找到 state.vscdb，跳过 VS Code 提取")
		return 0
	}
	if len(entries) == 0 {
		return 0
	}

	// 按类型统计并过滤掉文件类条目（信息量低，不生成）
	var folders, workspaces, files int
	kept := make([]vscodeEntry, 0, len(entries))
	for _, e := range entries {
		switch _, kind := classifyVSCode(e); kind {
		case vscodeFolder:
			folders++
		case vscodeWorkspace:
			workspaces++
		case vscodeFile:
			files++
			continue
		}
		kept = append(kept, e)
	}
	logInfo("VS Code 历史：共 %d 条（文件夹 %d、工作区 %d、文件 %d）",
		len(entries), folders, workspaces, files)
	if len(kept) > 0 {
		uris := make([]string, len(kept))
		for i, e := range kept {
			u, _ := classifyVSCode(e)
			uris[i] = u
		}
		logDebug("VS Code 条目明细：%s", strings.Join(uris, "、"))
	}
	if len(kept) == 0 {
		return 0
	}

	vscodeDir := filepath.Join(outputDir, "VSCode")
	os.MkdirAll(vscodeDir, 0755)

	before := listLnkNames(vscodeDir)
	runPowershell(buildVSCodeLnkScript(vscodeDir, codeExe, kept))

	created := lnkDiff(before, listLnkNames(vscodeDir))
	logInfo("已生成 %d 个快捷方式，跳过 %d 个文件类条目", len(created), files)
	if len(created) > 0 && len(created) <= 20 {
		names := make([]string, len(created))
		for i, name := range created {
			names[i] = strings.TrimSuffix(name, ".lnk")
		}
		logDebug("新建明细：%s", strings.Join(names, "、"))
	}
	return len(created)
}

// loadRecentEntries 读取单个 state.vscdb 的最近打开列表。
// 先拷贝到临时文件再读：VS Code 运行时持有原库连接，直接读可能撞上 SQLITE_BUSY。
func loadRecentEntries(dbPath string) ([]vscodeEntry, error) {
	data, err := os.ReadFile(dbPath)
	if err != nil {
		return nil, err
	}
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("vrtx-vscdb-%d.tmp", os.Getpid()))
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return nil, err
	}
	defer os.Remove(tmp)

	db, err := sql.Open("sqlite", tmp+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var value string
	err = db.QueryRow(`SELECT value FROM ItemTable WHERE key = 'history.recentlyOpenedPathsList'`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var h vscodeHistory
	if err := json.Unmarshal([]byte(value), &h); err != nil {
		return nil, err
	}

	out := make([]vscodeEntry, 0, len(h.Entries))
	for _, e := range h.Entries {
		if e.FolderURI == "" && e.FileURI == "" {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// classifyVSCode 判定条目 URI 与类型。工作区按后缀识别且优先于字段判断，
// 因为 .code-workspace 可能出现在 folderUri 字段里。
func classifyVSCode(e vscodeEntry) (uri string, kind vscodeKind) {
	uri = e.FolderURI
	if uri == "" {
		uri = e.FileURI
	}
	switch {
	case strings.HasSuffix(strings.ToLower(uri), ".code-workspace"):
		kind = vscodeWorkspace
	case e.FolderURI != "":
		kind = vscodeFolder
	default:
		kind = vscodeFile
	}
	return uri, kind
}

// uriBase 取路径部分的末段作为显示名（兼容 / 和 \ 分隔符）
func uriBase(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	return p
}

// maxShortcutName 限制显示名长度，防止超长导致 NTFS 路径超限
const maxShortcutName = 80

// vscodeShortcutName 生成一眼可分辨的快捷方式名：
//
//	【远程 · starlab-linux】distributed-and-cloud-computing.lnk
//	【远程 · starlab-linux】my-api【工作区】.lnk
//	【本地】my-project.lnk
//	【本地】notes【工作区】.lnk
//
// 不信任 VS Code 的 Label 字段——其值常为"完整路径 [SSH: 主机]"混合体，
// 是此前下划线汤的根源；一律从 URI 解析：主机取 authority 中 "+" 之后段，
// 名称取路径末段，Label 仅在解析结果为空时兜底。文件夹为默认形态不加后缀。
func vscodeShortcutName(e vscodeEntry) string {
	uri, kind := classifyVSCode(e)

	var scope, display string
	if rest, ok := strings.CutPrefix(uri, "vscode-remote://"); ok {
		authority, rawPath := rest, ""
		if i := strings.Index(rest, "/"); i >= 0 {
			authority, rawPath = rest[:i], rest[i+1:]
		}
		if dec, err := url.PathUnescape(authority); err == nil {
			authority = dec
		}
		host := authority
		if i := strings.LastIndex(authority, "+"); i >= 0 {
			host = authority[i+1:]
		}
		if host == "" {
			host = "remote"
		}
		scope = "【远程 · " + sanitizeFileName(host) + "】"

		if rawPath != "" {
			if dec, err := url.PathUnescape(rawPath); err == nil {
				rawPath = dec
			}
			display = uriBase(rawPath)
		}
	} else {
		scope = "【本地】"
		p := strings.TrimPrefix(uri, "file:///")
		if dec, err := url.PathUnescape(p); err == nil {
			p = dec
		}
		display = uriBase(p)
	}

	if display == "" && e.Label != "" {
		display = e.Label
	}
	if display == "" {
		display = "unnamed"
	}

	// 工作区条目去掉冗余的 .code-workspace 后缀，避免与【工作区】标签重复
	if kind == vscodeWorkspace {
		display = strings.TrimSuffix(strings.TrimSuffix(display, ".code-workspace"), ".CODE-WORKSPACE")
	}

	name := scope + truncateName(sanitizeFileName(display))
	if kind == vscodeWorkspace {
		name += "【工作区】"
	}
	return name
}

// truncateName 超长时截断显示名
func truncateName(s string) string {
	r := []rune(s)
	if len(r) <= maxShortcutName {
		return s
	}
	return string(r[:maxShortcutName])
}

// psSingleQuote 将字符串安全地包进 PowerShell 单引号字面量（” 转义内部单引号）
func psSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// buildVSCodeLnkScript 生成单个批量 PowerShell 脚本，
// 一次进程内创建全部快捷方式（与 shortcuts.go 的批处理模式一致，避免逐条起进程）。
// 同名条目自动追加 (2)(3) 序号去重，避免脚本内相互覆盖。
func buildVSCodeLnkScript(dir, codeExe string, entries []vscodeEntry) string {
	var b strings.Builder
	b.WriteString("$wshell = New-Object -ComObject WScript.Shell\n")

	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		base := vscodeShortcutName(e)
		name := base
		for i := 2; seen[name]; i++ {
			name = fmt.Sprintf("%s(%d)", base, i)
		}
		seen[name] = true

		uri, kind := classifyVSCode(e)
		flag := "--folder-uri"
		if kind != vscodeFolder {
			flag = "--file-uri"
		}
		args := fmt.Sprintf(`%s "%s"`, flag, uri)

		fmt.Fprintf(&b, "try {\n")
		fmt.Fprintf(&b, "  $s = $wshell.CreateShortcut(%s)\n", psSingleQuote(filepath.Join(dir, name+".lnk")))
		fmt.Fprintf(&b, "  $s.TargetPath = %s\n", psSingleQuote(codeExe))
		fmt.Fprintf(&b, "  $s.Arguments = %s\n", psSingleQuote(args))
		fmt.Fprintf(&b, "  $s.Save()\n")
		b.WriteString("} catch { }\n")
	}
	return b.String()
}

// lnkDiff 返回 after 相对 before 新增的 .lnk 文件名（排序保证确定性）
func lnkDiff(before, after map[string]struct{}) []string {
	var created []string
	for name := range after {
		if _, ok := before[name]; !ok {
			created = append(created, name)
		}
	}
	sort.Strings(created)
	return created
}
