# vrtx

vrtx（Vortex / 涡流）是一个 Windows 工具，用于聚合浏览器书签和系统快捷方式，统一输出为 `.url` 和 `.lnk` 文件，方便其他工具集成调用。

## 功能

### 浏览器书签

自动从 **Chrome** 和 **Edge** 的书签 JSON 文件中递归提取所有书签，按收藏夹目录结构命名，输出为 `.url` 快捷方式文件（可以直接双击打开）。

输出格式：`书签名称-收藏夹路径-域名.url`

### Windows 快捷方式

从以下路径提取 `.lnk` 快捷方式：

| 来源         | 路径                                                  |
| ------------ | ----------------------------------------------------- |
| 开始菜单     | `%APPDATA%\Microsoft\Windows\Start Menu\Programs`     |
| 公共开始菜单 | `%ProgramData%\Microsoft\Windows\Start Menu\Programs` |
| Windows Apps | 通过 COM 枚举 `shell:AppsFolder`                      |
| 最近文件     | `%APPDATA%\Microsoft\Windows\Recent`                  |
| Office 最近  | `%APPDATA%\Microsoft\Office\Recent`                   |
| 系统位置     | 回收站 / 此电脑 / 用户目录 / 开机启动（通过 `shell:`）|

### 文件监控

在监控模式下，定期检测书签文件和快捷方式目录的变更，自动增量重建输出。

## 使用

```
vrtx [选项]
```

### 选项

| 选项         | 默认值                | 说明                               |
| ------------ | --------------------- | ---------------------------------- |
| `-watch`     | `true`                | 启用监控模式，检测到变更时自动重建 |
| `-interval`  | `1s`                  | 监控轮询间隔                       |
| `-bookmarks` | `true`                | 提取浏览器书签                     |
| `-shortcuts` | `true`                | 提取 Windows 快捷方式              |
| `-out`       | `%TEMP%\VRTX`         | 输出目录                           |
| `-clean`     | `false`               | 清除所有输出文件后退出             |

### 示例

```bash
# 默认：提取全部并持续监控
vrtx

# 仅提取书签
vrtx -shortcuts=false

# 仅提取书签，不监控
vrtx -watch=false -shortcuts=false

# 仅提取快捷方式，不监控
vrtx -watch=false -bookmarks=false

# 自定义输出目录
vrtx -out D:\MyShortcuts

# 清理残留
vrtx -clean
```

## 输出结构

```
%TEMP%\VRTX\
├── Bookmarks\                  # 浏览器书签
│   ├── GitHub-开发-github.com.url
│   ├── YouTube-娱乐-youtube.com.url
│   └── ...
├── Shortcuts\                  # Windows 快捷方式
│   ├── StartMenu\              # 开始菜单
│   │   ├── 计算器.lnk
│   │   └── ...
│   ├── WindowsApps\            # Windows Apps
│   │   ├── Spotify.lnk
│   │   └── ...
│   ├── Recent\                 # 最近文件
│   │   └── ...
│   └── System\                 # 系统位置
    │       ├── 回收站.lnk
    │       ├── 此电脑.lnk
    │       ├── 用户目录.lnk
    │       └── 开机启动.lnk
```

## 构建

```bash
go build -ldflags="-s -w" -o vrtx.exe .
```

在项目目录下执行即可编译出 `vrtx.exe`。需要 Go 1.21+，无外部依赖。

Windows Apps 提取依赖 PowerShell（Windows 7+ 系统自带），其余功能使用纯 Go 实现。

## 许可证

GPLv3
