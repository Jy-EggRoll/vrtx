#Requires AutoHotkey v2.0

/**
 * 记录信息日志
 * @param str 要记录的日志内容
 * @param filePath 日志文件路径，默认 "*" 表示写入控制台
 * @param showConsole 如果控制台未打开，是否打开控制台
 */
LogInfo(str, filePath := "*", showConsole := false) {
    WriteLog("info", str, filePath, showConsole, 0x07)
}

/**
 * 记录警告日志
 * @param str 要记录的日志内容
 * @param filePath 日志文件路径，默认 "*" 表示写入控制台
 * @param showConsole 如果控制台未打开，是否打开控制台
 */
LogWarn(str, filePath := "*", showConsole := false) {
    WriteLog("warn", str, filePath, showConsole, 0x0E)
}

/**
 * 记录错误日志
 * @param ErrorObj 错误对象
 * @param filePath 日志文件路径，默认 "*" 表示写入控制台
 * @param showConsole 如果控制台未打开，是否打开控制台
 */
LogError(ErrorObj, filePath := "*", showConsole := false) {
    if showConsole {
        OpenConsole()
    } else if (filePath = "*") {
        return
    }

    timestamp := Format("[" A_YYYY "-" A_MM "-" A_DD " " A_Hour ":" A_Min ":" A_Sec "]")
    errorContent := timestamp " [error]`n"
    errorContent .= "    错误消息：" ErrorObj.Message "`n"
    errorContent .= "    错误位置：" ErrorObj.File "（第 " ErrorObj.Line " 行）`n"
    errorContent .= "    相关对象：" ErrorObj.What "`n"
    errorContent .= "    额外信息：" ErrorObj.Extra "`n"
    errorContent .= "`n"

    if (filePath = "*") {
        SetConsoleColor(0x0C)
        FileAppend(errorContent, filePath)
        ResetConsoleColor()
    } else {
        FileAppend(errorContent, filePath)
        LimitFileSize(filePath)
    }
}

/**
 * 记录日志并在 CMD 控制台中设置颜色
 */
WriteLog(level, str, filePath, showConsole, colorAttr) {
    if showConsole {
        OpenConsole()
    } else if (filePath = "*") {
        return
    }

    timestamp := Format("[" A_YYYY "-" A_MM "-" A_DD " " A_Hour ":" A_Min ":" A_Sec "]")
    logLine := timestamp " [" level "] " str "`n"

    if (filePath = "*") {
        SetConsoleColor(colorAttr)
        FileAppend(logLine, filePath)
        ResetConsoleColor()
    } else {
        FileAppend(logLine, filePath)
        LimitFileSize(filePath)
    }
}

/**
 * 设置控制台前景颜色
 * @param attr 控制台颜色属性
 */
SetConsoleColor(attr) {
    hConsole := DllCall("GetStdHandle", "int", -11, "ptr")
    if !hConsole || hConsole = -1
        return

    static defaultAttr := ""
    if (defaultAttr = "")
        defaultAttr := GetConsoleTextAttribute()

    DllCall("SetConsoleTextAttribute", "ptr", hConsole, "ushort", attr)
}

ResetConsoleColor() {
    hConsole := DllCall("GetStdHandle", "int", -11, "ptr")
    if !hConsole || hConsole = -1
        return

    static defaultAttr := ""
    if (defaultAttr = "")
        defaultAttr := GetConsoleTextAttribute()

    DllCall("SetConsoleTextAttribute", "ptr", hConsole, "ushort", defaultAttr)
}

GetConsoleTextAttribute() {
    hConsole := DllCall("GetStdHandle", "int", -11, "ptr")
    if !hConsole || hConsole = -1
        return 0x07

    csbi := Buffer(22)
    if DllCall("GetConsoleScreenBufferInfo", "ptr", hConsole, "ptr", csbi) {
        return NumGet(csbi, 16, "UShort")
    }

    return 0x07
}

/**
 * 打开控制台（如果尚未打开）
 */
OpenConsole() {
    handle := DllCall("GetStdHandle", "int", -11, "ptr")
    if !handle || handle = -1
        DllCall("AllocConsole")
}

/**
 * 限制日志文件大小，超过指定大小则轮转为 .1
 * @param filePath 日志文件路径
 * @param maxSizeInBytes 最大允许的文件大小，默认 1MiB，超过则轮转
 *
 * 原先超过上限是直接删除，等于丢掉全部历史，而这个日志的用途恰恰是回答
 * “上次为什么异常退出”，删除会让唯一的线索消失。改为轮转并保留一代，
 * 与 Go 侧 vrtx.log 的处理方式保持一致。
 */
LimitFileSize(filePath, maxSizeInBytes := 1024 * 1024) {
    if FileExist(filePath) {
        fileSize := FileGetSize(filePath)
        if (fileSize > maxSizeInBytes) {
            backup := filePath ".1"
            if FileExist(backup)
                FileDelete(backup)
            FileMove(filePath, backup)
        }
    }
}
