# vrtx

vrtx 是 Vortex（涡流）的缩写，当前旨在聚合 Windows 上的快捷方式、浏览器书签，方便我的其他工具集成调用。

最终，vrtx 的资源聚合目录可以改为用户 Temp 目录下的 eggroll-vrtx 文件夹。

AutoHotkey 提取的逻辑如下：

```ahk
InitShortcuts() {
    static initialized := false

    if initialized {
        return
    }
    initialized := true

    global shortcutsDir
    shortcutsDir := A_Temp "\WindowJump_Shortcuts"

    LogInfo("初始化快捷方式目录：" . shortcutsDir, , WindowJumpDebug.mode)

    if DirExist(shortcutsDir) {
        loop files, shortcutsDir "\*", "FD" {
            try FileDelete(A_LoopFileFullPath)
        }
    } else {
        DirCreate(shortcutsDir)
    }

    try {
        if DirExist(A_ProgramsCommon) {
            FileCopy(A_ProgramsCommon "\*.lnk", shortcutsDir "\", true)
        }
        if DirExist(A_Programs) {
            FileCopy(A_Programs "\*.lnk", shortcutsDir "\", true)
        }
    }

    try {
        oFolder := ComObject("Shell.Application").NameSpace("shell:AppsFolder")
        if (Type(oFolder) != "String") {
            for item in oFolder.Items {
                shortcutPath := shortcutsDir "\" item.Name ".lnk"
                if !FileExist(shortcutPath) {
                    try FileCreateShortcut("shell:appsfolder\" item.Path, shortcutPath)
                }
            }
        }
    }

    LogInfo("快捷方式初始化完成", , WindowJumpDebug.mode)
}

GetShortcuts(&shortcuts) {
    global shortcutsDir
    shortcuts := []

    if !DirExist(shortcutsDir) {
        LogInfo("快捷方式目录不存在，初始化", , WindowJumpDebug.mode)
        InitShortcuts()
    }

    loop files, shortcutsDir "\*.lnk", "F" {
        try {
            name := StrReplace(A_LoopFileName, ".lnk", "")
            shortcuts.Push({ name: name, path: A_LoopFileFullPath })
        }
    }

    LogInfo("获取到 " . shortcuts.Length . " 个快捷方式", , WindowJumpDebug.mode)
}
```

现需要将这个逻辑迁移到 go，并最好使用并发来加速文件提取操作。

还需要加入涡流的路径有：（带 Roaming 的那个，我不确定我的环境变量是对的）

- `%APPDATA%\Microsoft\Windows\Recent`
- `%APPDATA%\Microsoft\Office\Recent`

功能二：

浏览器书签提取。

浏览器的书签本质上就是一个 json 文件，路径如下：

`C:\Users\EggRoll\AppData\Local\Microsoft\Edge\User Data\Default\Bookmarks`

其中，其字段如下：

```json
{
  "checksum": "d41d8cd98f00b204e9800998ecf8427e",
  "roots": {
    "bookmark_bar": {
      "children": [
        {
          "date_added": "13217472000000000",
          "id": "1",
          "name": "Example Bookmark",
          "type": "url",
          "url": "https://www.example.com"
        }
      ],
      "date_added": "13217472000000000",
      "date_modified": "13217472000000000",
      "id": "0",
      "name": "Bookmarks Bar",
      "type": "folder"
    },
    // 其他根节点...
  },
  // 其他字段...
}
```

需要利用 go 的强大 json 解析能力和并发能力，快速把所有的 url 提取出来，构造成 Windows 的 url 快捷方式，方便直接双击打开。
