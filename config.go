package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// 配置文件名：与 exe 同目录的 vrtx.json
const configFileName = "vrtx.json"

// 轮询间隔的合法范围（秒）
const (
	minIntervalSeconds = 1
	maxIntervalSeconds = 3600
)

// ExtractConfig 控制各提取类别的启停
type ExtractConfig struct {
	Bookmarks bool `json:"bookmarks"`
	Software  bool `json:"software"`
	System    bool `json:"system"`
	Drives    bool `json:"drives"`
	Recent    bool `json:"recent"`
	Office    bool `json:"office"`
	VSCode    bool `json:"vscode"`
}

// Config 是 vrtx 的全部运行时行为配置
type Config struct {
	OutputDir       string        `json:"output_dir"`       // 空 = 默认 %TEMP%\VRTX
	IntervalSeconds int           `json:"interval_seconds"` // 监控轮询间隔
	Watch           bool          `json:"watch"`            // 监控模式总开关
	Extract         ExtractConfig `json:"extract"`          // 各类别开关
}

// defaultConfig 唯一事实源：首次启动、恢复默认、损坏兜底都从这里取值
func defaultConfig() *Config {
	return &Config{
		OutputDir:       "",
		IntervalSeconds: 1,
		Watch:           true,
		Extract: ExtractConfig{
			Bookmarks: true,
			Software:  true,
			System:    true,
			Drives:    true,
			Recent:    false,
			Office:    false,
			VSCode:    true,
		},
	}
}

// sanitize 归位非法值，保证内存与磁盘上的配置永远合法
func (c *Config) sanitize() {
	if c.IntervalSeconds < minIntervalSeconds {
		c.IntervalSeconds = minIntervalSeconds
	}
	if c.IntervalSeconds > maxIntervalSeconds {
		c.IntervalSeconds = maxIntervalSeconds
	}
}

// Interval 返回轮询间隔时长
func (c *Config) Interval() time.Duration {
	return time.Duration(c.IntervalSeconds) * time.Second
}

// OutputPath 解析实际输出目录（未配置时回退默认临时目录）
func (c *Config) OutputPath() string {
	if c.OutputDir == "" {
		return getOutputDir()
	}
	return c.OutputDir
}

var (
	configPath string
	cfgPtr     atomic.Pointer[Config]
)

// current 返回当前生效配置的快照指针
func current() *Config { return cfgPtr.Load() }

// initConfig 定位 exe 同目录的 vrtx.json 并加载；不存在则创建默认配置
func initConfig() {
	dir, err := os.Executable()
	if err != nil {
		logWarn("无法定位可执行文件，配置文件将放在工作目录：%v", err)
		dir = "."
	}
	configPath = filepath.Join(filepath.Dir(filepath.Clean(dir)), configFileName)

	cfg := loadConfigFile(configPath)
	cfgPtr.Store(cfg)

	// 启动即落盘：首跑生成默认文件；老文件补齐缺失字段并规范化
	if err := saveConfig(cfg); err != nil {
		logWarn("无法写入配置文件（exe 位于只读目录？），改动仅本次运行生效：%v", err)
	} else {
		logInfo("配置文件：%s", configPath)
	}
}

// loadConfigFile 加载配置：默认值打底 + JSON 覆盖合并；
// 仅在启动时调用一次——运行中修改 vrtx.json 需重启生效。
// 文件不存在或损坏时不炸程序，回退默认值且不覆盖坏文件
func loadConfigFile(path string) *Config {
	def := defaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logWarn("读取配置失败，使用默认配置：%v", err)
		}
		return def
	}
	c := *def
	c.Extract = def.Extract
	if err := json.Unmarshal(data, &c); err != nil {
		logWarn("解析配置失败（%v），使用默认配置", err)
		return def
	}
	c.sanitize()
	return &c
}

// saveConfig 原子写配置文件（临时文件 + rename）
func saveConfig(c *Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := configPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, configPath)
}

// updateConfig 应用新配置：先落盘再换指针；落盘失败仅告警，运行时照常生效
func updateConfig(c *Config) error {
	c.sanitize()
	if err := saveConfig(c); err != nil {
		logWarn("保存配置文件失败（本次运行仍生效）：%v", err)
	}
	cfgPtr.Store(c)
	return nil
}

// modifiedFields 对比当前值与默认值，返回每个配置项是否被修改过。
// 键为扁平路径（如 "extract.vscode"），供网页面板渲染修改标记。
func modifiedFields(cur, def *Config) map[string]bool {
	m := map[string]bool{
		"output_dir":        cur.OutputDir != def.OutputDir,
		"interval_seconds":  cur.IntervalSeconds != def.IntervalSeconds,
		"watch":             cur.Watch != def.Watch,
		"extract.bookmarks": cur.Extract.Bookmarks != def.Extract.Bookmarks,
		"extract.software":  cur.Extract.Software != def.Extract.Software,
		"extract.system":    cur.Extract.System != def.Extract.System,
		"extract.drives":    cur.Extract.Drives != def.Extract.Drives,
		"extract.recent":    cur.Extract.Recent != def.Extract.Recent,
		"extract.office":    cur.Extract.Office != def.Extract.Office,
		"extract.vscode":    cur.Extract.VSCode != def.Extract.VSCode,
	}
	return m
}
