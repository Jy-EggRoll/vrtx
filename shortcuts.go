//go:build windows

package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// powershell 封装 PowerShell 调用，固定 -NoProfile -NonInteractive -ExecutionPolicy Bypass 参数
// 以避免用户配置文件干扰、交互弹框和执行策略限制
func powershell(script string) *exec.Cmd {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-Command", script)
	hideWindow(cmd)
	return cmd
}

// runPowershell 执行一段 PowerShell 脚本（可附带额外环境变量），失败时记录警告。
// 统一了各快捷方式提取函数里重复的 CombinedOutput + logWarn 样板。
func runPowershell(script string, env ...string) {
	cmd := powershell(script)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		logWarn("PowerShell 执行失败: %v\n%s", err, out)
	}
}

// extractShortcuts 按开关并发启动已启用的提取任务：
//
//	software → StartMenu/ + WindowsApps/
//	recent   → Recent/    office → Office/
//	system   → System/    drives → Drives/
//
// 各任务独立并行，全部完成后返回
func extractShortcuts(outputDir string, software, system, drives, recent, office bool) {
	shortcutDir := filepath.Join(outputDir, "Shortcuts")
	os.MkdirAll(shortcutDir, 0755)

	var tasks []func()
	if software {
		tasks = append(tasks,
			func() { extractStartMenuShortcuts(shortcutDir) },
			func() { extractWindowsAppsShortcuts(shortcutDir) },
		)
	}
	if recent {
		tasks = append(tasks, func() { extractRecentShortcuts(shortcutDir) })
	}
	if office {
		tasks = append(tasks, func() { extractOfficeShortcuts(shortcutDir) })
	}
	if system {
		tasks = append(tasks, func() { extractSystemShortcuts(shortcutDir) })
	}
	if drives {
		tasks = append(tasks, func() { extractDriveShortcuts(shortcutDir) })
	}
	runConcurrent(tasks...)
}

// startMenuDirs 返回开始菜单 Programs 目录（用户态 + 机器态），多处复用
func startMenuDirs() []string {
	homeDir, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Windows", "Start Menu", "Programs"),
	}
	if programData := os.Getenv("ProgramData"); programData != "" {
		dirs = append(dirs, filepath.Join(programData, "Microsoft", "Windows", "Start Menu", "Programs"))
	}
	return dirs
}

func extractStartMenuShortcuts(shortcutDir string) {
	targetDir := filepath.Join(shortcutDir, "StartMenu")
	os.MkdirAll(targetDir, 0755)

	total := 0
	for _, dir := range startMenuDirs() {
		total += copyLnkFiles(dir, targetDir)
	}
	logInfo("开始菜单：写入 %d 个快捷方式", total)
}

// listLnkNames 列出目录下现有 .lnk 文件名集合，用于 PowerShell 执行前后对比统计新建数量
func listLnkNames(dir string) map[string]struct{} {
	names := make(map[string]struct{})
	entries, err := os.ReadDir(dir)
	if err != nil {
		return names
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".lnk") {
			names[e.Name()] = struct{}{}
		}
	}
	return names
}

// reportNewLnks 对比执行前后的 .lnk 集合，报告新建数量（少量时以 debug 级列出明细）。
// 返回新建数量，供调用方聚合统计。
func reportNewLnks(label string, before, after map[string]struct{}) int {
	var created []string
	for name := range after {
		if _, ok := before[name]; !ok {
			created = append(created, name)
		}
	}
	sort.Strings(created)
	logInfo("%s：新建 %d 个快捷方式", label, len(created))
	if len(created) > 0 && len(created) <= 20 {
		names := make([]string, len(created))
		for i, name := range created {
			names[i] = strings.TrimSuffix(name, ".lnk")
		}
		logDebug("新建明细：%s", strings.Join(names, "、"))
	}
	return len(created)
}

// extractWindowsAppsShortcuts 通过 COM 枚举 shell:AppsFolder 虚拟文件夹，
// 使用 PowerShell 创建 .lnk 快捷方式。
// 之所以不走文件系统，是因为 Windows Apps 不在常规磁盘路径中，
// 只能通过 Shell.Application COM 接口枚举。
func extractWindowsAppsShortcuts(shortcutDir string) {
	targetDir := filepath.Join(shortcutDir, "WindowsApps")
	os.MkdirAll(targetDir, 0755)

	before := listLnkNames(targetDir)
	script := `
$dir = [Environment]::GetEnvironmentVariable("VRTX_APPS_DIR")
$shell = New-Object -ComObject Shell.Application
$folder = $shell.NameSpace("shell:AppsFolder")
$wshell = New-Object -ComObject WScript.Shell
foreach ($item in $folder.Items()) {
    $name = $item.Name
    $name = [regex]::Replace($name, '[<>:"/\|?*]', '_')
    $path = Join-Path $dir "$name.lnk"
    if (!(Test-Path $path)) {
        try {
            $s = $wshell.CreateShortcut($path)
            $s.TargetPath = "shell:appsfolder\" + $item.Path
            $s.Save()
        } catch { }
    }
}
`
	runPowershell(script, "VRTX_APPS_DIR="+targetDir)
	reportNewLnks("Windows Apps", before, listLnkNames(targetDir))
}

// extractRecentShortcuts 复制 Windows 最近文件夹中的 .lnk 快捷方式。
func extractRecentShortcuts(shortcutDir string) {
	homeDir, _ := os.UserHomeDir()
	targetDir := filepath.Join(shortcutDir, "Recent")
	os.MkdirAll(targetDir, 0755)

	n := copyLnkFiles(filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Windows", "Recent"), targetDir)
	logInfo("最近文件：写入 %d 个快捷方式", n)
}

// extractOfficeShortcuts 复制 Office 最近文件夹中的 .lnk 快捷方式。
func extractOfficeShortcuts(shortcutDir string) {
	homeDir, _ := os.UserHomeDir()
	targetDir := filepath.Join(shortcutDir, "Office")
	os.MkdirAll(targetDir, 0755)

	n := copyLnkFiles(filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Office", "Recent"), targetDir)
	logInfo("Office 最近：写入 %d 个快捷方式", n)
}

func extractSystemShortcuts(shortcutDir string) {
	targetDir := filepath.Join(shortcutDir, "System")
	os.MkdirAll(targetDir, 0755)

	before := listLnkNames(targetDir)
	script := `
$dir = [Environment]::GetEnvironmentVariable("VRTX_SYS_DIR")
$wshell = New-Object -ComObject WScript.Shell

$items = @(
    @{Name="回收站"; Path="shell:RecycleBinFolder"},
    @{Name="此电脑"; Path="shell:MyComputerFolder"},
    @{Name="用户目录"; Path="shell:UsersFilesFolder"},
    @{Name="开机启动"; Path="shell:Startup"}
)

foreach ($item in $items) {
    $path = Join-Path $dir ($item.Name + ".lnk")
    if (!(Test-Path $path)) {
        try {
            $s = $wshell.CreateShortcut($path)
            $s.TargetPath = "explorer.exe"
            $s.Arguments = $item.Path
            $s.Save()
        } catch { }
    }
}
`
	runPowershell(script, "VRTX_SYS_DIR="+targetDir)
	reportNewLnks("系统位置", before, listLnkNames(targetDir))
}

// extractDriveShortcuts 为每个可用盘符创建 .lnk 快捷方式。
// 盘符探测使用 os.Stat 检查根目录是否存在，从 C 开始（跳过 A/B 传统软驱）。
func extractDriveShortcuts(shortcutDir string) {
	targetDir := filepath.Join(shortcutDir, "Drives")
	os.MkdirAll(targetDir, 0755)

	drives := getAvailableDrives()
	if len(drives) == 0 {
		return
	}

	before := listLnkNames(targetDir)
	script := `
$dir = [Environment]::GetEnvironmentVariable("VRTX_DRIVES_DIR")
$drives = [Environment]::GetEnvironmentVariable("VRTX_DRIVE_LIST") -split ','
$wshell = New-Object -ComObject WScript.Shell
foreach ($drive in $drives) {
    $name = $drive
    $path = Join-Path $dir ($name + ".lnk")
    if (!(Test-Path $path)) {
        try {
            $s = $wshell.CreateShortcut($path)
            $s.TargetPath = $drive + ":\"
            $s.Save()
        } catch { }
    }
}
`
	runPowershell(script,
		"VRTX_DRIVES_DIR="+targetDir,
		"VRTX_DRIVE_LIST="+strings.Join(drives, ","),
	)
	reportNewLnks("磁盘驱动器", before, listLnkNames(targetDir))
}

// getAvailableDrives 检测当前系统可用的盘符。
// 从 C 开始（跳过 A/B，这对传统软驱盘符在现代系统上几乎不会出现），
// 通过 os.Stat 检查根目录是否存在来判断。
func getAvailableDrives() []string {
	var drives []string
	for _, letter := range "CDEFGHIJKLMNOPQRSTUVWXYZ" {
		path := string(letter) + ":\\"
		if _, err := os.Stat(path); err == nil {
			drives = append(drives, string(letter))
		}
	}
	return drives
}

// copyLnkFiles 递归遍历 srcDir，将 .lnk 文件按原目录结构复制到 dstDir。
// 用 getUniquePath 处理同名冲突。返回成功复制的文件数。
func copyLnkFiles(srcDir, dstDir string) int {
	copied := 0
	filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".lnk") {
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return nil
		}
		dst := filepath.Join(dstDir, relPath)
		os.MkdirAll(filepath.Dir(dst), 0755)
		dst = getUniquePath(dst)

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			logWarn("复制快捷方式失败: %v", err)
			return nil
		}
		copied++
		logDebug("复制 %s", relPath)
		return nil
	})
	return copied
}

// getShortcutSrcDirs 返回需要监控变更的快捷方式源目录。
// 按开关拼接：software → 开始菜单两处 Programs；recent → Windows Recent；office → Office Recent。
// system 和 drives 无文件系统源，不在此列表（drives 依赖盘符变化检测）。
func getShortcutSrcDirs(software, recent, office bool) []string {
	var dirs []string
	if software {
		dirs = append(dirs, startMenuDirs()...)
	}
	homeDir, _ := os.UserHomeDir()
	if recent {
		dirs = append(dirs, filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Windows", "Recent"))
	}
	if office {
		dirs = append(dirs, filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Office", "Recent"))
	}
	return dirs
}
