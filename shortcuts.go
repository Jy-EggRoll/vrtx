package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

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
    @{Name="控制面板"; Path="shell:ControlPanelFolder"}
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
