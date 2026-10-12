#Requires AutoHotkey v2.0
#SingleInstance Force
#WinActivateForce
#NoTrayIcon

; vrtx 看门狗：父进程退出则自动结束脚本
if A_Args.Length >= 1 {
    vrtxParentPid := Integer(A_Args[1])
    VrtxCheckParent := (*) => (ProcessExist(vrtxParentPid) ? 0 : ExitApp())
    SetTimer VrtxCheckParent, 1000
}

#Include ./LoggerLib/Logger.ahk
#Include ./VDLib/VD.ahk
#Include ./WindowStyleLib/WindowStyle.ahk
#Include ./PinYinLib/IbPinyin.ahk

; 全局配置
WindowJumpPinyinPartialMatch := true
WindowJumpShortcutLabel := "【软件】"
WindowJumpBookmarkLabel := "【书签】"
VRTX_BASE := A_Temp "\VRTX"
WindowJumpPrevActiveHwnd := 0

; 图标缓存放在文件级而不是函数 static：EditBox 的 Change 回调是个闭包，它需要
; 把缓存引用交给搜索函数，而“闭包对外层函数的 static 取引用”这一组合官方文档
; 没有正面描述（Functions.htm 只说嵌套函数可以引用外层的局部与 static 变量），
; 提升为全局后各处按名引用，不再依赖解释器对这一组合的处理方式。
iconCache := Map()
shortcutCache := Map()

; AHK 侧的异常日志与 Go 侧的 vrtx.log 放在同一目录，便于对照时间线；
; 用独立文件名是因为两个进程各自持有句柄，写同一个文件会互相干扰。
; 目录从环境变量取：AHK 只有 A_AppData，它指向 Roaming，没有对应的
; LOCALAPPDATA 内建变量，而 Go 侧的 os.UserCacheDir() 用的正是后者。
; 环境变量缺失时退回临时目录，避免把日志写到当前盘根目录。
VRTX_LOG_DIR := EnvGet("LOCALAPPDATA")
if (VRTX_LOG_DIR = "")
    VRTX_LOG_DIR := A_Temp
VRTX_LOG_DIR .= "\VRTX"
WindowJumpLogFile := VRTX_LOG_DIR "\ahk.log"
if !DirExist(VRTX_LOG_DIR)
    DirCreate(VRTX_LOG_DIR)

; 评分权重配置
ScoreCfg := {
    Exact: 1000,
    ExactStart: 200,
    Pinyin: 800,
    PinyinStart: 200,
    TargetLenFactor: 100,
    CatWeight: Map("Shortcuts", 500, "Bookmarks", 0),
}

; 启动标记：与 Go 侧的启动、退出标记配对，用来确认 AHK 是否曾正常运行
LogInfo("WindowJump.ahk 已启动", WindowJumpLogFile)

UpdateTheme()
InitShortcuts()

; 初始化：确保 VRTX 目录存在
InitShortcuts() {
    static initialized := false
    if initialized
        return
    initialized := true
    global VRTX_BASE
    if !DirExist(VRTX_BASE) {
        DirCreate(VRTX_BASE)
    }
}

; 递归获取目录下所有 .lnk 和 .url 文件
GetAllShortcutFiles(dir) {
    files := []
    if !DirExist(dir)
        return files
    loop files, dir "\*.lnk", "F"
        files.Push(A_LoopFileFullPath)
    loop files, dir "\*.url", "F"
        files.Push(A_LoopFileFullPath)
    loop files, dir "\*", "D" {
        subFiles := GetAllShortcutFiles(A_LoopFileFullPath)
        for _, f in subFiles
            files.Push(f)
    }
    return files
}

; 搜索窗口（默认模式）
SearchWindows(query) {
    results := []
    searchLower := StrLower(query)
    bak_DetectHiddenWindows := A_DetectHiddenWindows
    A_DetectHiddenWindows := true
    for hwnd in WinGetList() {
        try {
            if (!IsValidWindow(hwnd))
                continue
            desktopNum := VD.getDesktopNumOfWindow("ahk_id " . hwnd)
            if (desktopNum < 1)
                continue
            process := WinGetProcessName(hwnd)
            title := WinGetTitle(hwnd)
            fullText := StrLower("[" . process . "] " . title)
            if (query == "") {
                score := 1
            } else {
                score := FuzzyScore(searchLower, fullText)
            }
            if (score > 0) {
                desktopInfo := " [桌面" . desktopNum . "]"
                results.Push({
                    score: score,
                    text: desktopInfo . " [" . process . "] " . title,
                    hwnd: hwnd,
                    isShortcut: false,
                    isAdmin: false,
                    iconProcess: process
                })
            }
        }
    }
    A_DetectHiddenWindows := bak_DetectHiddenWindows
    return results
}

; 搜索 VRTX 指定类别
; adminOnly = false（默认）：只返回普通软件；adminOnly = true：只返回管理员软件
SearchVRTXCategory(query, category, label, adminOnly := false) {
    global VRTX_BASE
    results := []
    searchLower := StrLower(query)
    targetDir := VRTX_BASE "\" category
    if !DirExist(targetDir)
        return results
    allFiles := GetAllShortcutFiles(targetDir)
    for _, fullPath in allFiles {
        SplitPath(fullPath, &fileName)
        name := StrReplace(fileName, ".lnk", "")
        name := StrReplace(name, ".url", "")
        if (query == "") {
            score := 1
        } else {
            fullText := StrLower(name)
            score := FuzzyScore(searchLower, fullText)
        }
        if (score > 0) {
            catWeight := ScoreCfg.CatWeight
            score += catWeight[category]
            if (adminOnly) {
                results.Push({
                    score: score,
                    text: "【管理员】" . label . " " . name,
                    hwnd: fullPath,
                    isShortcut: true,
                    isAdmin: true,
                    iconProcess: ""
                })
            } else {
                results.Push({
                    score: score,
                    text: label . " " . name,
                    hwnd: fullPath,
                    isShortcut: true,
                    isAdmin: false,
                    iconProcess: ""
                })
            }
        }
    }
    return results
}

; 浮层跟随鼠标所在显示器，并按该屏的缩放比例渲染。AutoHotkey v2.0 对这两件事都没有
; 现成做法：Gui.Show("Center") 取的是主显示器的工作区，写死在源码里；解释器的清单是
; System DPI Aware，窗口移到别的缩放比例上会被 Windows 按位图拉伸，字就糊。随包分发的
; vrtxahk.exe 不能改，所以位置、尺寸、字体与图标都只能自己算。
;
; 下面这个函数把当前线程临时提到 per-monitor（-3），消除位图拉伸与坐标换算，返回先前的
; 上下文供还原。不用 -4 是因为它会连带缩放非客户区，而 v2.0 的 Gui 不响应 WM_DPICHANGED；
; 浮层无边框、每次唤出都重新定位，用 -3 已经够。
SetPerMonitorContext() {
    static unavailable := false
    if (unavailable)
        return 0
    try {
        return DllCall("SetThreadDpiAwarenessContext", "ptr", -3, "ptr")
    } catch {
        ; Windows 10 1607 之前没有这个导出：退回原样，浮层仍跟随显示器，只是会糊
        unavailable := true
        return 0
    }
}

RestoreDpiContext(prev) {
    if (prev)
        DllCall("SetThreadDpiAwarenessContext", "ptr", prev, "ptr")
}

; 取目标显示器的工作区与缩放比例，算好浮层的尺寸、坐标与字号。
; 调用前必须已经在 per-monitor 上下文里，返回的坐标才是该屏的物理像素（见 WindowJump）。
GetOverlayLayout() {
    ; 96 DPI 下的设计尺寸，实际尺寸按目标屏缩放比例放大
    static DESIGN_W := 600
    static DESIGN_H := 450
    static DESIGN_FONT := 12
    ; 图标是字号像素高的多少倍，稍大于 1 才不会显得比文字小
    static ICON_FONT_RATIO := 1.15

    ; MouseGetPos 默认给的是相对活动窗口的坐标，这里要屏幕坐标
    CoordMode "Mouse", "Screen"
    MouseGetPos &mx, &my

    ; 鼠标所在的显示器；鼠标落在显示器之间的空隙时退回主屏
    monitor := 0
    Loop MonitorGetCount() {
        MonitorGet A_Index, &left, &top, &right, &bottom
        if (mx >= left && mx < right && my >= top && my < bottom) {
            monitor := A_Index
            break
        }
    }
    if (monitor = 0)
        monitor := MonitorGetPrimary()

    ; 工作区已排除任务栏，比主屏的 SPI_GETWORKAREA 更贴合目标屏
    MonitorGetWorkArea monitor, &workLeft, &workTop, &workRight, &workBottom

    dpi := ProbeMonitorDpi(workLeft, workTop)
    ; 缩放既要跟显示器 DPI，也要受屏幕尺寸约束：竖屏或小屏配高缩放时，
    ; 只按 DPI 放大可能让浮层超出屏幕边缘。取三者最小值即可兼顾。
    scale := Min(dpi / 96, (workRight - workLeft) / DESIGN_W, (workBottom - workTop) / DESIGN_H)
    width := Round(DESIGN_W * scale)
    height := Round(DESIGN_H * scale)
    overlayX := workLeft + (workRight - workLeft - width) // 2
    overlayY := workTop + (workBottom - workTop - height) // 2

    ; 多显示器出问题时先看这一行：选了哪块屏、按什么缩放、落在哪个坐标。
    ; 它只用于排查，写失败不该拖累唤出（LoggerLib 内部不吞异常）。
    try LogInfo("浮层定位 显示器" . monitor . " DPI" . dpi . " 缩放" . scale . " 位置" . overlayX . "," . overlayY, WindowJumpLogFile)

    ; 12pt 在 96 DPI 下是 16 像素高，按 scale 缩放后反推点数（见 GetFontPixelsPerPoint）
    fontSize := Round(DESIGN_FONT * 96 * scale / GetFontPixelsPerPoint())
    ; 图标按字号像素高的固定倍数算，不必去试行高
    iconSize := Round(DESIGN_FONT * 96 / 72 * scale * ICON_FONT_RATIO)

    return {
        dpi: dpi,
        scale: scale,
        width: width,
        height: height,
        x: overlayX,
        y: overlayY,
        font: fontSize,
        iconSize: iconSize
    }
}

; 目标显示器的有效 DPI。现成的取法都不牢靠：GetDpiForMonitor 随调用进程的 DPI 感知
; 变化，本进程是 System DPI Aware，在副屏上只能拿到主屏的值；GetDpiForWindow 对别人
; 的窗口会返回它创建时的 DPI，同样可能过期。所以摆一个自己的窗口到目标屏上再问它。
ProbeMonitorDpi(workLeft, workTop) {
    probe := 0
    try {
        probe := Gui("-Caption +ToolWindow -DPIScale")
        probe.Show("x" . workLeft . " y" . workTop . " w1 h1 NoActivate")
        dpi := DllCall("GetDpiForWindow", "ptr", probe.Hwnd, "uint")
        if (dpi >= 96)
            return dpi
    } catch Error as e {
        LogError(e, WindowJumpLogFile)
    } finally {
        ; 探针只为问一句话，任何路径下都不该留在屏幕上
        if (probe)
            probe.Destroy()
    }
    return A_ScreenDPI
}

; SetFont 把点数换算成像素高度时，取的是屏幕设备上下文报出的 LOGPIXELSY，这个值与
; 浮层所在显示器的 DPI 未必相等，也不受 Gui 的 DPIScale 开关影响。所以按同样的方式
; 量一次，再反推能让字体落在目标像素高度上的点数。
GetFontPixelsPerPoint() {
    hdc := DllCall("GetDC", "ptr", 0, "ptr")
    pixelsPerPoint := DllCall("GetDeviceCaps", "ptr", hdc, "int", 90)   ; 90 = LOGPIXELSY
    DllCall("ReleaseDC", "ptr", 0, "ptr", hdc)
    return pixelsPerPoint > 0 ? pixelsPerPoint : 96
}

; 按目标尺寸建图标列表，让图标跟字体一起缩放。IL_Create 只能选系统的小图标或大图标
; 两档（它在源码里把"任意尺寸"留给了未来的参数），所以直接用 ImageList_Create：
; IL_Add 与 SetImageList 收的都是 HIMAGELIST 句柄，自建的同样能用；而且 IL_Add 会先读
; 列表的实际尺寸再按该尺寸加载图标，所以放大之后依然是清晰的。
CreateIconList(iconSize) {
    static ILC_MASK := 0x1
    static ILC_COLOR32 := 0x20
    return DllCall("ImageList_Create", "int", iconSize, "int", iconSize, "uint", ILC_MASK | ILC_COLOR32, "int", 10, "int", 5, "ptr")
}

; 主入口
WindowJump(pinyinPartialMatch := "") {
    global WindowJumpPinyinPartialMatch
    if (pinyinPartialMatch !== "") {
        WindowJumpPinyinPartialMatch := pinyinPartialMatch
    }
    UpdateTheme()
    InitShortcuts()

    static MyGui := 0
    static hIL := 0
    static lastTheme := ""
    static lastAccent := ""
    static lastScale := 0
    static focusCheck := 0

    global WindowJumpPrevActiveHwnd
    try {
        prevHwnd := WinExist("A")
        if (prevHwnd && (!MyGui || prevHwnd != MyGui.Hwnd)) {
            WindowJumpPrevActiveHwnd := prevHwnd
        }
    } catch Error as e {
        LogError(e, WindowJumpLogFile)
    }

    ; 浮层的坐标、尺寸与字体都按目标屏计算，全程必须在 per-monitor 上下文里做；
    ; finally 把线程还原，避免这套上下文泄漏到后续热键线程、改变其它调用的坐标语义。
    prevCtx := SetPerMonitorContext()
    try {
        layout := GetOverlayLayout()
        global IsDarkMode, AccentColor, iconCache, shortcutCache
        px := (v) => Round(v * layout.scale)
        currentTheme := IsDarkMode ? "dark" : "light"
        currentAccent := AccentColor

        if (MyGui) {
            themeChanged := (lastTheme != "" && lastTheme != currentTheme)
            accentChanged := (lastAccent != "" && lastAccent != currentAccent)
            ; 缩放比例变了必须重建：控件尺寸与字体在创建时就已固化，事后改 DPIScale
            ; 或重设字体都不会重排既有控件（官方文档明确说明这一点）。
            scaleChanged := (lastScale != layout.scale)

            if (themeChanged || accentChanged || scaleChanged) {
                MyGui.Destroy()
                MyGui := 0
                hIL := 0
                iconCache := Map()
                shortcutCache := Map()
            }
        }
        lastTheme := currentTheme
        lastAccent := currentAccent
        ; 在重建判断之后更新，否则重建完仍留着旧值，下次唤出会再重建一次
        lastScale := layout.scale

        if (MyGui) {
            try {
                reusePrev := WinExist("A")
                if (reusePrev && reusePrev != MyGui.Hwnd) {
                    WindowJumpPrevActiveHwnd := reusePrev
                }
            } catch Error as e {
                LogError(e, WindowJumpLogFile)
            }
            MyGui["SearchInput"].Value := ""
            MyGui["SearchInput"].Focus()
            RefreshAllWindows(MyGui["ResultList"], hIL, iconCache)
            MyGui.Show("x" . layout.x . " y" . layout.y)
            return
        }

        MyGui := Gui("-Caption +AlwaysOnTop +Owner +LastFound -DPIScale", "QuickSwitcher")
        MyGui.BackColor := BgColor
        MyGui.SetFont("s" . layout.font . " c" . FontColor, "微软雅黑")

        radius := px(20)
        WinSetRegion("0-0 w" . layout.width . " h" . layout.height . " r" . radius . "-" . radius, MyGui.Hwnd)

        MyGui.Add("Text", "x" . px(25) . " y" . px(15) . " h" . px(30) . " c" . AccentColor, "快速跳转 | Del 关闭窗口 | s 软件 | b 书签 | as 管理员")

        EditBox := MyGui.Add("Edit", "x" . px(20) . " y" . px(45) . " w" . px(560) . " h" . px(22) . " vSearchInput -E0x200 Background" . ListViewBg)

        hIL := CreateIconList(layout.iconSize)

        ResultList := MyGui.Add("ListView", "x" . px(20) . " y" . px(95) . " w" . px(560) . " r14 -Multi -Hdr -E0x200 vResultList +LV0x140 Background" .
            ListViewBg . " c" . FontColor, ["Display", "HWND", "IsShortcut", "IsAdmin"])

        ; 显式指定 small：图标尺寸大于系统小图标时，不指定类型会被当成大图标列表，
        ; 而 report 视图只认 small，图标就显示不出来
        ResultList.SetImageList(hIL, 1)
        ResultList.ModifyCol(1, px(540))
        ResultList.ModifyCol(2, 0)
        ResultList.ModifyCol(3, 0)
        ResultList.ModifyCol(4, 0)

        RefreshAllWindows(ResultList, hIL, iconCache)

        EditBox.OnEvent("Change", (obj, *) => ScheduleSearch(obj, MyGui["ResultList"], hIL))
        ResultList.OnEvent("DoubleClick", (obj, row) => ActivateWin(obj, row))

        HotIfWinActive("ahk_id " . MyGui.Hwnd)
        Hotkey("Escape", (*) => CancelSwitcher(MyGui), "On")
        Hotkey("Down", (*) => MoveLVSelection(MyGui["ResultList"], "Down"), "On")
        Hotkey("Up", (*) => MoveLVSelection(MyGui["ResultList"], "Up"), "On")
        Hotkey("Enter", (*) => HandleEnter(MyGui), "On")
        Hotkey("Delete", (*) => CloseSelectedWindow(MyGui["ResultList"]), "On")

        ; 定时器要先停掉旧的再挂新的：直接 SetTimer 一个新回调对象，AHK 会把它当成
        ; 另一个定时器，旧的那个仍在跑，于是一次重建就多一个。
        if (focusCheck)
            SetTimer(focusCheck, 0)
        focusCheck := () => CheckWinFocus(MyGui)
        SetTimer(focusCheck, 100)

        MyGui.Show("x" . layout.x . " y" . layout.y . " w" . layout.width . " h" . layout.height)
    } finally {
        RestoreDpiContext(prevCtx)
    }
}

CloseSelectedWindow(LV) {
    row := LV.GetNext(0, "Focused")
    if (row == 0) {
        row := LV.GetNext(0)
    }
    if (row == 0 && LV.GetCount() > 0) {
        row := 1
    }
    if (row > 0) {
        hwnd := LV.GetText(row, 2)
        isShortcut := LV.GetText(row, 3) = "1"
        if (hwnd && !isShortcut) {
            ; 临时开启 DetectHiddenWindows，保证可以跨虚拟桌面关闭软件窗口
            bakDetectHidden := A_DetectHiddenWindows
            DetectHiddenWindows(true)
            PostMessage(0x10, 0, 0, , "ahk_id " . hwnd)
            DetectHiddenWindows(bakDetectHidden)
            LV.Delete(row)
            if (LV.GetCount() > 0) {
                nextRow := (row > LV.GetCount()) ? LV.GetCount() : row
                LV.Modify(nextRow, "Select Focus Vis")
            }
        }
    }
}

ScheduleSearch(EditObj, LV, hIL) {
    static timer := 0
    if (timer) {
        SetTimer(timer, 0)
    }
    timer := SetTimer(() => UpdateSearch(EditObj, LV, hIL), -20)
}

UpdateSearch(EditObj, LV, hIL) {
    global WindowJumpShortcutLabel, WindowJumpBookmarkLabel
    rawInput := EditObj.Value
    LV.Delete()

    ; 检测 VRTX 是否可用（目录存在）
    vrtxAvailable := DirExist(VRTX_BASE)

    mode := "window"
    searchQuery := rawInput

    ; 仅当 VRTX 可用时才处理前缀
    if (vrtxAvailable) {
        prefixMap := Map("as ", "admin", "b ", "bookmark", "s ", "shortcut")
        for prefix, m in prefixMap {
            if (SubStr(rawInput, 1, StrLen(prefix)) = prefix) {
                mode := m
                searchQuery := Trim(SubStr(rawInput, StrLen(prefix) + 1))
                break
            }
        }
        ; 后缀模式：支持 "feishu b"、"steam s" 等写法
        if (mode = "window") {
            suffixMap := Map(" as", "admin", " b", "bookmark", " s", "shortcut")
            for suffix, m in suffixMap {
                if (SubStr(rawInput, -StrLen(suffix)) = suffix) {
                    mode := m
                    searchQuery := Trim(SubStr(rawInput, 1, StrLen(rawInput) - StrLen(suffix)))
                    break
                }
            }
        }
    }

    results := []
    if (mode = "window") {
        results := SearchWindows(searchQuery)
        ; 窗口搜索无结果时，自动进入启动器模式搜索软件+书签（不含管理员）
        if (results.Length = 0 && vrtxAvailable) {
            softwareResults := SearchVRTXCategory(searchQuery, "Shortcuts", WindowJumpShortcutLabel, false)
            bookmarkResults := SearchVRTXCategory(searchQuery, "Bookmarks", WindowJumpBookmarkLabel, false)
            for _, r in softwareResults
                results.Push(r)
            for _, r in bookmarkResults
                results.Push(r)
        }
    } else if (mode = "bookmark") {
        results := SearchVRTXCategory(searchQuery, "Bookmarks", WindowJumpBookmarkLabel, false)
    } else if (mode = "shortcut") {
        results := SearchVRTXCategory(searchQuery, "Shortcuts", WindowJumpShortcutLabel, false)
    } else if (mode = "admin") {
        results := SearchVRTXCategory(searchQuery, "Shortcuts", WindowJumpShortcutLabel, true)
    }

    if (results.Length > 0) {
        ; 排序
        loop results.Length {
            i := A_Index
            while (i > 1 && results[i - 1].score < results[i].score) {
                temp := results[i]
                results[i] := results[i - 1]
                results[i - 1] := temp
                i--
            }
        }
        maxShow := Min(results.Length, 30)
        loop maxShow {
            res := results[A_Index]
            iconIdx := 1
            if (res.isShortcut) {
                if (shortcutCache.Has(res.hwnd)) {
                    iconIdx := shortcutCache[res.hwnd]
                } else {
                    iconIdx := GetFileIconIndex(res.hwnd, hIL)
                    shortcutCache[res.hwnd] := iconIdx
                }
            } else {
                if (res.iconProcess != "") {
                    iconIdx := GetIconIndexByProcess(res.iconProcess, hIL, iconCache)
                }
            }
            LV.Add("Icon" . iconIdx, res.text, res.hwnd, res.isShortcut ? "1" : "0",
                res.isAdmin ? "1" : "0")
        }
    }

    if (LV.GetCount() > 0) {
        LV.Modify(1, "Select Focus")
    }
}

CancelSwitcher(guiObj) {
    global WindowJumpPrevActiveHwnd
    guiObj.Hide()
    try {
        if (WindowJumpPrevActiveHwnd && WinExist("ahk_id " . WindowJumpPrevActiveHwnd)
        && WindowJumpPrevActiveHwnd != guiObj.Hwnd) {
            DllCall("AllowSetForegroundWindow", "int", -1)
            Sleep 50
            WinActivate("ahk_id " . WindowJumpPrevActiveHwnd)
            if (!WinActive("ahk_id " . WindowJumpPrevActiveHwnd)) {
                Sleep 50
                WinActivate("ahk_id " . WindowJumpPrevActiveHwnd)
            }
        }
    } catch Error as e {
        LogError(e, WindowJumpLogFile)
    }
}

CheckWinFocus(guiObj) {
    ; 浮层重建期间 MyGui 会短暂为 0，而定时器还在按 100ms 触发
    if (!guiObj)
        return
    if !WinExist("ahk_id " . guiObj.Hwnd) {
        return
    }
    if !WinActive("ahk_id " . guiObj.Hwnd) {
        guiObj.Hide()
    }
}

RefreshAllWindows(LV, hIL, iconCache) {
    LV.Delete()
    windowCount := 0
    bak_DetectHiddenWindows := A_DetectHiddenWindows
    A_DetectHiddenWindows := true
    for hwnd in WinGetList() {
        try {
            if (IsValidWindow(hwnd)) {
                desktopNum := VD.getDesktopNumOfWindow("ahk_id " . hwnd)
                if (desktopNum < 1) {
                    continue
                }
                desktopInfo := " [桌面" . desktopNum . "]"
                title := WinGetTitle(hwnd)
                process := WinGetProcessName(hwnd)
                iconIdx := GetIconIndexByProcess(process, hIL, iconCache)
                LV.Add("Icon" . iconIdx, desktopInfo . " [" . process . "] " . title, hwnd)
                windowCount++
            }
        }
    }
    A_DetectHiddenWindows := bak_DetectHiddenWindows
    if (LV.GetCount() > 0) {
        LV.Modify(1, "Select Focus")
    }
}

GetIconIndexByProcess(process, hIL, iconCache) {
    if (iconCache.Has(process)) {
        return iconCache[process]
    }
    hwnd := WinExist("ahk_exe " . process)
    if (!hwnd) {
        return 1
    }
    iconIdx := GetUwpIconIndex(hwnd, hIL)
    iconCache[process] := iconIdx
    return iconIdx
}

GetUwpIconIndex(hwnd, hIL) {
    try {
        exePath := WinGetProcessPath(hwnd)
        if exePath {
            if (StrEndsWith(exePath, "ApplicationFrameHost.exe")) {
                return GetUwpIconFromWindow(hwnd, hIL)
            }
            return GetExeIconIndex(exePath, hIL)
        }
    }
    return 1
}

GetFileIconIndex(filePath, hIL) {
    try {
        if (StrEndsWith(filePath, ".lnk")) {
            FileGetShortcut filePath, &targetPath, &workDir, &args, &desc, &iconFile, &iconNum
            if (iconFile) {
                iconPath := iconFile
                if (iconNum > 0) {
                    return IL_Add(hIL, iconPath, iconNum)
                } else {
                    return IL_Add(hIL, iconPath)
                }
            }
            if (targetPath) {
                filePath := targetPath
            }
        }
    }
    return GetExeIconIndex(filePath, hIL)
}

GetExeIconIndex(filePath, hIL) {
    try {
        fisize := A_PtrSize + 688
        fileinfo := Buffer(fisize)
        if DllCall("shell32\SHGetFileInfoW", "WStr", filePath, "UInt", 0, "Ptr", fileinfo, "UInt", fisize, "UInt",
            0x100) {
            hIcon := NumGet(fileinfo, 0, "Ptr")
            if hIcon {
                ; SHGetFileInfo 交出来的图标要由调用方释放，否则每取一个图标就漏一个
                idx := IL_Add(hIL, "HICON:" . hIcon)
                DllCall("DestroyIcon", "ptr", hIcon)
                return idx
            }
        }
    }
    ; IL_Add 失败会返回 0，而 ListView 的 Icon 选项不接受 0，用它填充会直接抛错
    idx := IL_Add(hIL, filePath)
    return idx ? idx : 1
}

GetUwpIconFromWindow(hwnd, hIL) {
    hIcon := SendMessage(0x7F, 0, 0, hwnd, "ahk_id " . hwnd)
    if !hIcon {
        hIcon := SendMessage(0x7F, 1, 0, hwnd, "ahk_id " . hwnd)
    }
    if !hIcon {
        hIcon := DllCall(A_PtrSize == 8 ? "GetClassLongPtr" : "GetClassLong", "Ptr", hwnd, "Int", -34, "UPtr")
    }
    if hIcon {
        return IL_Add(hIL, "HICON:" . hIcon)
    }
    return 1
}

StrEndsWith(str, suffix) {
    return SubStr(str, -StrLen(suffix) + 1) = suffix
}

FuzzyScore(query, target) {
    if !(query := Trim(query)) {
        return 0
    }
    target := StrReplace(target, ".exe", "")
    totalScore := 0
    matchedTokens := 0
    tokens := StrSplit(query, " ")
    for _, token in tokens {
        if (token == "") {
            continue
        }
        tokenScore := 0
        pinyinFlags := IbPinyin_AsciiFirstLetter | IbPinyin_Ascii
        if (WindowJumpPinyinPartialMatch) {
            pinyinFlags |= IbPinyin_PatternPartial
        }
        if InStr(target, token) {
            tokenScore := ScoreCfg.Exact
            if InStr(target, token, true, 1, 1) {
                tokenScore += ScoreCfg.ExactStart
            }
        } else if IbPinyin_FindMatch(token, target, &start, &end, pinyinFlags) {
            tokenScore := ScoreCfg.Pinyin
            if (start == 1) {
                tokenScore += ScoreCfg.PinyinStart
            }
        }
        if (tokenScore > 0) {
            totalScore += tokenScore
            matchedTokens++
        }
    }
    if (matchedTokens < tokens.Length) {
        return 0
    }
    ; target 越短加分越高（精确匹配 > 模糊匹配）
    ; 分母至少取 1：扫描 VRTX 目录时可能遇到空名条目，除零会直接抛错
    totalScore += Round(StrLen(query) / Max(StrLen(target), 1) * ScoreCfg.TargetLenFactor)
    return totalScore
}

MoveLVSelection(LV, Direction) {
    if (LV.GetCount() == 0) {
        return
    }
    row := LV.GetNext(0, "Focused")
    if (row == 0) {
        row := LV.GetNext(0)
    }
    if (Direction == "Down") {
        nextRow := (row == 0) ? 1 : Min(row + 1, LV.GetCount())
    } else {
        nextRow := (row == 0) ? LV.GetCount() : Max(row - 1, 1)
    }
    LV.Modify(0, "-Select -Focus")
    LV.Modify(nextRow, "Select Focus Vis")
}

HandleEnter(GuiObj) {
    LV := GuiObj["ResultList"]
    row := LV.GetNext(0, "Focused")
    if (row == 0) {
        row := LV.GetNext(0)
    }
    if (row == 0 && LV.GetCount() > 0) {
        row := 1
    }
    if (row > 0) {
        ActivateWin(LV, row)
    }
}

ActivateWin(LV, RowNumber) {
    try {
        hwnd := LV.GetText(RowNumber, 2)
        isShortcut := LV.GetText(RowNumber, 3) = "1"
        isAdmin := LV.GetText(RowNumber, 4) = "1"
        if (hwnd) {
            if (isShortcut) {
                if (isAdmin) {
                    LV.Gui.Hide()
                    Sleep 50
                    AdminRun(hwnd)
                } else {
                    LV.Gui.Hide()
                    Sleep 50
                    UserRun(hwnd)
                }
            } else {
                global lastActiveWindowClass
                lastActiveWindowClass := "AutoHotkeyGUI"
                targetDesktopNum := VD.getDesktopNumOfWindow("ahk_id " . hwnd)
                currentDesktopNum := VD.getCurrentDesktopNum()
                if (targetDesktopNum > 0 && targetDesktopNum != currentDesktopNum) {
                    LV.Gui.Hide()
                    Sleep 50
                    VD.goToDesktopOfWindow("ahk_id " . hwnd)
                } else {
                    LV.Gui.Hide()
                    Sleep 50
                    WinActivate("ahk_id " . hwnd)
                }
            }
        }
    } catch Error as e {
        LogError(e, WindowJumpLogFile)
    }
}

AdminRun(Target) {
    try {
        DllCall("Shell32\ShellExecuteW", "Ptr", 0, "Str", "runas", "Str", Target, "Ptr", 0, "Ptr", 0, "Int", 1)
    } catch as e {
        ; LogError 期望的是 Error 对象，直接传字符串会让它在读取 .Message 等
        ; 属性时再次抛错。把上下文写进 Extra，仍然按错误对象记录
        e.Extra := "RunAsAdmin 失败"
        LogError(e, WindowJumpLogFile)
    }
}

UserRun(Target, Args := "", WorkingDir := "") {
    try {
        shellApp := ComObject("Shell.Application")
        shellWindows := shellApp.Windows
        desktop := shellWindows.FindWindowSW(0, 0, 8, 0, 1)
        if (desktop) {
            DllCall("AllowSetForegroundWindow", "int", -1)
            desktop.Document.Application.ShellExecute(Target, Args, WorkingDir, "open", 1)
        }
    } catch Error as e {
        LogError(e, WindowJumpLogFile)
    }
}

UpdateTheme() {
    global BgColor, FontColor, AccentColor, ListViewBg, IsDarkMode
    try {
        IsDarkMode := RegRead("HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize",
            "AppsUseLightTheme") == 0
    } catch {
        IsDarkMode := false
    }
    try {
        rawColor := RegRead("HKEY_CURRENT_USER\Software\Microsoft\Windows\DWM", "AccentColor")
        r := rawColor & 0xFF
        g := (rawColor >> 8) & 0xFF
        b := (rawColor >> 16) & 0xFF
        accentNum := (r << 16) | (g << 8) | b
        AccentColor := Format("{:06X}", accentNum)
    } catch {
        accentNum := 0x0078D7
        AccentColor := "0078D7"
    }
    if (IsDarkMode) {
        BgColor := MixColor(accentNum, 0x111111, 0.90)
        FontColor := "c6c6c6"
        ListViewBg := MixColor(accentNum, 0x111111, 0.80)
    } else {
        BgColor := MixColor(accentNum, 0xFFFFFF, 0.90)
        FontColor := "333333"
        ListViewBg := MixColor(accentNum, 0xFFFFFF, 0.80)
    }
}

MixColor(Color1, Color2, Weight) {
    r1 := (Color1 >> 16) & 0xFF
    g1 := (Color1 >> 8) & 0xFF
    b1 := Color1 & 0xFF
    r2 := (Color2 >> 16) & 0xFF
    g2 := (Color2 >> 8) & 0xFF
    b2 := Color2 & 0xFF
    r := Round(r1 + (r2 - r1) * Weight)
    g := Round(g1 + (g2 - g1) * Weight)
    b := Round(b1 + (b2 - b1) * Weight)
    return Format("{:02X}{:02X}{:02X}", r, g, b)
}

; 全局特殊热键绑定
; Ctrl 双击触发
ctrlIsPressed := false
lastCtrlPressTime := 0

~Ctrl:: {
    global ctrlIsPressed, lastCtrlPressTime
    if (ctrlIsPressed)
        return
    ctrlIsPressed := true
    if (lastCtrlPressTime
        && A_TickCount - lastCtrlPressTime <= 250
        && InStr(A_PriorKey, "Control")) {
        WindowJump()
        lastCtrlPressTime := 0
    } else {
        lastCtrlPressTime := A_TickCount
    }
}

~Ctrl Up:: {
    global ctrlIsPressed
    ctrlIsPressed := false
}
