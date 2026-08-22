; ============================================================
; VRTX 集成看门狗 —— 由 vrtx.exe 启动时自动注入调用，勿删
;
; vrtx 主程序以普通权限运行，本脚本经 UAC 以管理员权限运行；
; 跨提权边界主程序无法结束子进程，故由脚本侧每秒轮询父进程，
; 父进程退出（正常退出 / 崩溃 / 被任务管理器结束）即自动跟随退出。
;
; 调用约定：AutoHotkey64.exe WindowJump.ahk <VRTX主程序PID>
; ============================================================

if A_Args.Length >= 1 {
    vrtxParentPid := Integer(A_Args[1])
    VrtxCheckParent := (*) => (ProcessExist(vrtxParentPid) ? 0 : ExitApp())
    SetTimer VrtxCheckParent, 1000
}
