//go:build windows

package main

import (
	"io/fs"
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

	var wg sync.WaitGroup

	if software {
		wg.Add(1)
		go func() {
			defer wg.Done()
			extractSoftwareShortcuts(shortcutDir)
		}()
	}

	if recent {
		wg.Add(1)
		go func() {
			defer wg.Done()
			extractRecentShortcuts(shortcutDir)
		}()
	}

	if office {
		wg.Add(1)
		go func() {
			defer wg.Done()
			extractOfficeShortcuts(shortcutDir)
		}()
	}

	if system {
		wg.Add(1)
		go func() {
			defer wg.Done()
			extractSystemShortcuts(shortcutDir)
		}()
	}

	if drives {
		wg.Add(1)
		go func() {
			defer wg.Done()
			extractDriveShortcuts(shortcutDir)
		}()
	}

	wg.Wait()
}

// extractSoftwareShortcuts 并发执行软件相关的两个提取任务：
// 开始菜单的 .lnk 快捷方式和 Windows Apps 的 COM 枚举。
func extractSoftwareShortcuts(shortcutDir string) {
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

// extractRecentShortcuts 复制 Windows 最近文件夹中的 .lnk 快捷方式。
func extractRecentShortcuts(shortcutDir string) {
	homeDir, _ := os.UserHomeDir()

	targetDir := filepath.Join(shortcutDir, "Recent")
	os.MkdirAll(targetDir, 0755)

	copyLnkFiles(filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Windows", "Recent"), targetDir)
}

// extractOfficeShortcuts 复制 Office 最近文件夹中的 .lnk 快捷方式。
func extractOfficeShortcuts(shortcutDir string) {
	homeDir, _ := os.UserHomeDir()

	targetDir := filepath.Join(shortcutDir, "Office")
	os.MkdirAll(targetDir, 0755)

	copyLnkFiles(filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Office", "Recent"), targetDir)
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

// copyLnkFiles 递归遍历 srcDir，将 .lnk 文件按原目录结构复制到 dstDir。
// 用 getUniquePath 处理同名冲突。
func copyLnkFiles(srcDir, dstDir string) {
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
		}
		return nil
	})
}

// getShortcutSrcDirs 返回需要监控变更的快捷方式源目录。
// 按开关拼接：software → 开始菜单两处 Programs；recent → Windows Recent；office → Office Recent。
// system 和 drives 无文件系统源，不在此列表（drives 依赖盘符变化检测）。
func getShortcutSrcDirs(software, recent, office bool) []string {
	homeDir, _ := os.UserHomeDir()
	programData := os.Getenv("ProgramData")

	var dirs []string

	if software {
		dirs = append(dirs, filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Windows", "Start Menu", "Programs"))
		if programData != "" {
			dirs = append(dirs, filepath.Join(programData, "Microsoft", "Windows", "Start Menu", "Programs"))
		}
	}

	if recent {
		dirs = append(dirs, filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Windows", "Recent"))
	}

	if office {
		dirs = append(dirs, filepath.Join(homeDir, "AppData", "Roaming", "Microsoft", "Office", "Recent"))
	}

	return dirs
}
