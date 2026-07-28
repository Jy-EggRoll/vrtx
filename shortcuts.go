//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// powershell 封装 PowerShell 调用，固定 -NoProfile -NonInteractive -ExecutionPolicy Bypass 参数
// 以避免用户配置文件干扰、交互弹框和执行策略限制
func powershell(script string) *exec.Cmd {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	hideWindow(cmd)
	return cmd
}

// extractShortcuts 并发启动 5 个提取任务：
//   开始菜单 → StartMenu/    Windows Apps → WindowsApps/
//   最近文件  → Recent/       系统位置   → System/
//   磁盘根目录 → Drives/
// 各任务独立并行，全部完成后返回
func extractShortcuts(outputDir string) {
	shortcutDir := filepath.Join(outputDir, "Shortcuts")
	os.MkdirAll(shortcutDir, 0755)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		extractStartMenuShortcuts(shortcutDir)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		extractWindowsAppsShortcuts(shortcutDir)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		extractRecentShortcuts(shortcutDir)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		extractSystemShortcuts(shortcutDir)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		extractDriveShortcuts(shortcutDir)
	}()

	wg.Wait()
}

func extractStartMenuShortcuts(shortcutDir string) {
	homeDir, _ := os.UserHomeDir()
	programData := os.Getenv("ProgramData")

	dirs := []string{
		filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Windows", "Start Menu", "Programs"),
	}
	if programData != "" {
		dirs = append(dirs, filepath.Join(programData, "Microsoft", "Windows", "Start Menu", "Programs"))
	}

	targetDir := filepath.Join(shortcutDir, "StartMenu")
	os.MkdirAll(targetDir, 0755)

	for _, dir := range dirs {
		copyLnkFiles(dir, targetDir)
	}
}

// extractWindowsAppsShortcuts 通过 COM 枚举 shell:AppsFolder 虚拟文件夹，
// 使用 PowerShell 创建 .lnk 快捷方式。
// 之所以不走文件系统，是因为 Windows Apps 不在常规磁盘路径中，
// 只能通过 Shell.Application COM 接口枚举。
func extractWindowsAppsShortcuts(shortcutDir string) {
	targetDir := filepath.Join(shortcutDir, "WindowsApps")
	os.MkdirAll(targetDir, 0755)

	cmd := exec.Command("powershell", "-NoProfile", "-Command", `
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
`)
	cmd.Env = append(os.Environ(), "VRTX_APPS_DIR="+targetDir)

	out, err := cmd.CombinedOutput()
	if err != nil {
		logWarn("提取 Windows Apps 失败: %v\n%s", err, out)
	}
}

func extractRecentShortcuts(shortcutDir string) {
	homeDir, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Windows", "Recent"),
		filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Office", "Recent"),
	}

	targetDir := filepath.Join(shortcutDir, "Recent")
	os.MkdirAll(targetDir, 0755)

	for _, dir := range dirs {
		copyLnkFiles(dir, targetDir)
	}
}

func extractSystemShortcuts(shortcutDir string) {
	targetDir := filepath.Join(shortcutDir, "System")
	os.MkdirAll(targetDir, 0755)

	cmd := exec.Command("powershell", "-NoProfile", "-Command", `
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
`)
	cmd.Env = append(os.Environ(), "VRTX_SYS_DIR="+targetDir)

	out, err := cmd.CombinedOutput()
	if err != nil {
		logWarn("提取系统快捷方式失败: %v\n%s", err, out)
	}
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

	driveList := strings.Join(drives, ",")

	cmd := exec.Command("powershell", "-NoProfile", "-Command", `
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
`)
	cmd.Env = append(os.Environ(),
		"VRTX_DRIVES_DIR="+targetDir,
		"VRTX_DRIVE_LIST="+driveList,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		logWarn("提取驱动器快捷方式失败: %v\n%s", err, out)
	}
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

// copyLnkFiles 复制 srcDir 下的 .lnk 文件到 dstDir。
// 只复制文件（跳过子目录），用 getUniquePath 处理同名冲突。
func copyLnkFiles(srcDir, dstDir string) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".lnk") {
			continue
		}

		src := filepath.Join(srcDir, entry.Name())
		dst := filepath.Join(dstDir, entry.Name())
		dst = getUniquePath(dst)

		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			logWarn("复制快捷方式失败: %v", err)
		}
	}
}

func getShortcutSrcDirs() []string {
	homeDir, _ := os.UserHomeDir()
	programData := os.Getenv("ProgramData")

	dirs := []string{
		filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Windows", "Start Menu", "Programs"),
		filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Windows", "Recent"),
		filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Office", "Recent"),
	}
	if programData != "" {
		dirs = append(dirs, filepath.Join(programData, "Microsoft", "Windows", "Start Menu", "Programs"))
	}

	return dirs
}
