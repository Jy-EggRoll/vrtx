# VRTX（涡流）

VRTX 是一个 Windows 常驻工具：把散落在系统各处的「入口」——浏览器书签、开始菜单软件、磁盘盘符、VS Code 本地与远程会话——统一提取为 `.url` / `.lnk` 快捷方式，聚合到同一个目录树中，配合 [Everything](https://www.voidtools.com/) 等即时搜索工具实现**全局一键直达**。

**核心特性**

- **浏览器书签**：Chrome / Edge 书签递归提取为 `.url` 文件
- **Windows 快捷方式**：开始菜单、Windows Apps、最近文件、系统位置、全部盘符
- **VS Code 连接**：本地文件夹、工作区、SSH / WSL 远程会话，一键回到上次的开发环境
- **网页实时控制台**：SSE 推送日志、级别筛选、Catppuccin 主题自适应深浅色
- **网页设置面板**：运行时调整行为即刻生效，修改项高亮标记、支持单项重置
- **系统托盘常驻**：单击开控制台，右键直达设置与清理
- **开机自启开关**：托盘菜单一键切换，自动修复指向旧路径的失效启动项
- **AutoHotkey 集成**：可选随主程序自动拉起 WindowJump 脚本（UAC 授权，主程序退出后自动跟随结束）；解释器以 `vrtxahk.exe` 之名随包分发，来源一目了然

## 快速开始

1. 双击 `vrtx.exe`，托盘区出现图标（程序单实例运行，重复启动会被拒绝）
2. 右键托盘图标 →「打开设置」，按需勾选提取类别
3. 右键托盘图标 →「打开控制台」查看实时日志；输出目录中的快捷方式即刻可用

默认输出目录为 `%TEMP%\VRTX`，可在设置面板中修改。

## 与 Everything 搭配使用效果最佳

VRTX 的设计初衷就是做 Everything 的**弹药库**：

- VRTX 把书签、软件、VS Code 会话等一切入口**物化为普通文件**（`.url` / `.lnk`），集中在一个目录树里
- [Everything](https://www.voidtools.com/) 以 NTFS 原生速度索引文件名，输入即出结果
- 二者结合后：**按下 Everything 热键 → 敲几个字母 → 回车**，就能到达任何一个书签、任何一款软件、任何一台远程开发机上的任意项目——全程不碰浏览器收藏栏、不翻开始菜单、不打开 VS Code 的欢迎页

推荐用法：

1. 保持 VRTX 默认输出目录不变（或自定义一个短路径，如 `R:\Temp\VRTX`）
2. Everything 默认全盘索引，无需额外配置；如排除了 TEMP 目录，请将 VRTX 输出目录加入索引
3. 日常检索示例：
   - `github url` → 所有 GitHub 相关书签
   - `ext:lnk starlab` → 所有指向 starlab-linux 远程主机的快捷方式
   - `my-config` → 本地配置仓库与远程同名项目一并命中
4. 在 Everything 中直接双击结果即可打开对应的书签 / 软件 / VS Code 会话

> 提示：VS Code 远程连接快捷方式依赖本机已安装 VS Code 并配置好对应 SSH / WSL 环境。

## 功能详解

### 浏览器书签

自动从 **Chrome** 和 **Edge** 的书签 JSON 中递归提取所有书签，按收藏夹层级命名，生成可直接双击打开的 `.url` 文件。

命名格式：`书签名-收藏夹路径-域名.url`

### Windows 快捷方式

| 来源 | 路径 / 方式 |
| --- | --- |
| 开始菜单 | `%APPDATA%\Microsoft\Windows\Start Menu\Programs` |
| 公共开始菜单 | `%ProgramData%\Microsoft\Windows\Start Menu\Programs` |
| Windows Apps | COM 枚举 `shell:AppsFolder` |
| 最近文件 | `%APPDATA%\Microsoft\Windows\Recent` |
| Office 最近 | `%APPDATA%\Microsoft\Office\Recent` |
| 系统位置 | 回收站 / 此电脑 / 用户目录 / 开机启动 |
| 磁盘根目录 | 全部可用盘符（`C.lnk`、`D.lnk`…U 盘即插即识别） |

### VS Code 连接

从 VS Code 的 `state.vscdb`（同时支持 Insiders 版）读取最近打开记录，为每个**文件夹**和**工作区**生成快捷方式，双击即以对应的本地 / SSH / WSL 目标重新打开 VS Code：

```
【远程 · starlab-linux】distributed-and-cloud-computing.lnk   ← SSH 远程项目
【远程 · Ubuntu-22.04】GitRepo【工作区】.lnk                  ← WSL 工作区
【本地】vrtx.lnk                                              ← 本地文件夹
【本地】notes【工作区】.lnk                                   ← 本地工作区
```

单个文件的打开记录不生成快捷方式。

### 文件监控

监控模式下按可配置间隔轮询各数据源：书签文件 mtime、快捷方式来源目录、可用盘符集合、VS Code 历史数据库。检测到变更时精确报告触发源并自动重建对应输出。

## 网页控制台与设置

### 控制台

- 实时日志经 SSE 推送，无需刷新
- 级别筛选：`全部` / `INFO+` / `WARN+`，明细日志以 DEBUG 级别展示
- Catppuccin 配色，跟随系统深浅色模式自动切换

### 设置面板

托盘右键「打开设置」进入。修改保存后**立即生效**，无需重启：

- 各提取类别开关（停用的类别保留已生成输出）
- 监控开关与轮询间隔
- 输出目录热切换（旧目录自动清空迁移）
- 被修改过的项以黄色标记显示，可单项重置或一键恢复默认
- 「立即清理并重建」按钮

### 配置文件 `vrtx.json`

位于 exe 同目录，首次启动自动生成。**WebUI 是运行中调整行为的唯一入口**；直接编辑此文件需重启程序后生效。

```json
{
  "output_dir": "",
  "interval_seconds": 1,
  "watch": true,
  "extract": {
    "bookmarks": true,
    "software": true,
    "system": true,
    "drives": true,
    "recent": false,
    "office": false,
    "vscode": true
  }
}
```

| 字段 | 说明 |
| --- | --- |
| `output_dir` | 输出目录，留空使用 `%TEMP%\VRTX` |
| `interval_seconds` | 监控轮询间隔（1–3600 秒） |
| `watch` | 监控模式总开关 |
| `extract.*` | 各提取类别开关 |

## 输出结构

```
%TEMP%\VRTX\
├── Bookmarks\                  # 浏览器书签 (.url)
│   ├── GitHub-开发-github.com.url
│   └── ...
├── Shortcuts\                  # Windows 快捷方式 (.lnk)
│   ├── StartMenu\
│   ├── WindowsApps\
│   ├── Recent\
│   ├── Office\
│   ├── System\
│   └── Drives\
│       ├── C.lnk
│       └── ...
└── VSCode\                     # VS Code 连接 (.lnk)
    ├── 【远程 · starlab-linux】my-project.lnk
    ├── 【本地】vrtx.lnk
    └── ...
```

## 安全模型

VRTX 对输出目录执行严格的所有权校验：**只会写入和清空「空目录」或「完全由 VRTX 创建的内容」**。若配置的目录包含任何其他文件（例如误指向了你的项目仓库），程序将拒绝写入、拒绝删除并明确列出冲突条目——宁可拒绝服务，绝不误删数据。

## 构建

```bash
task build-all
```

- 需要 Go 1.26+ 与 [Task](https://taskfile.dev/)；`goversioninfo` 由任务流程调用，用于生成图标、版本信息与 DPI 清单资源（直接 `go build` 得到的 exe 不含这些资源）
- 无 CGO 依赖，支持交叉编译：发布产物为安装器 `vrtx-setup-x64.exe`（Inno Setup，per-user 安装、中英双语向导）与便携包 `build/vrtx-windows-amd64.zip` / `build/vrtx-windows-arm64.zip`（内含 `vrtx.exe` 与完整 `ahk/` 运行时，解压即用）
- **ARM64 说明**：ARM64 构建并非全组件原生适配——AutoHotkey 解释器与拼音库等仍为 x64 版本，依赖 Windows 的 x64 转译层运行，可能存在不稳定或未知的问题；请酌情慎重使用，ARM64 设备建议直接使用压缩包（便携）版本
- 运行依赖：Windows 10+，PowerShell（Windows Apps / 系统位置 / 盘符 / VS Code 快捷方式经由 PowerShell 生成）
- 命令行仅支持 `--version` / `-v` 查看版本

## 许可证

GPLv3
