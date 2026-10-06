; VRTX 安装器脚本（Inno Setup 6）
;
; 编译参数（由 CI 传入，本地调试可用 ISCC 手工指定）：
;   /DStage       待安装文件所在目录（须含 vrtx.exe 与 ahk\ 目录）
;   /DAppVersion  版本号字符串，如 1.2.3
;   /O<dir>       输出目录覆盖

#define AppName "VRTX"
#define AppExeName "vrtx.exe"

#ifndef AppVersion
#define AppVersion "0.0.0"
#endif

[Setup]
AppId={{7F3A9C42-1B8D-4E56-9A0D-C5E28B71F604}
AppName={#AppName}
AppVersion={#AppVersion}
DefaultDirName={localappdata}\Programs\{#AppName}
PrivilegesRequired=lowest
OutputDir=dist
OutputBaseFilename=vrtx-setup-x64
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
UninstallDisplayIcon={app}\{#AppExeName}
DisableProgramGroupPage=yes

[Languages]
Name: "chinese"; MessagesFile: "ChineseSimplified.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; \
    Description: "{cm:CreateDesktopIcon}"; \
    GroupDescription: "{cm:AdditionalIcons}"; \
    Flags: unchecked

[Files]
Source: "{#Stage}\vrtx.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#Stage}\ahk\*"; DestDir: "{app}\ahk"; \
    Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{autoprograms}\VRTX\{#AppName}"; Filename: "{app}\{#AppExeName}"
Name: "{autoprograms}\VRTX\卸载 {#AppName}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#AppExeName}"; \
    Description: "{cm:LaunchProgram,{#AppName}}"; \
    Flags: nowait postinstall skipifsilent unchecked

[UninstallDelete]
; 运行期产生的配置与日志；自启动开关可能创建的 lnk 一并清理（不存在则静默跳过）
Type: files; Name: "{app}\vrtx.json"
Type: filesandordirs; Name: "{localappdata}\VRTX"
Type: files; Name: "{userappdata}\Microsoft\Windows\Start Menu\Programs\Startup\VRTX.lnk"

[Code]
// 卸载时删除开机自启动任务。
//
// 放在代码段而不是 [UninstallRun]，是为了按执行结果决定是否提示用户：
// 任务计划程序把任务存放在 System32\Tasks 下，标准用户无权访问，而本安装器
// 以普通权限安装、卸载时同样是普通权限，启用 UAC 的机器上这一步会失败。
// 失败时若只是静默跳过，残留任务会在每次开机时尝试运行一个已被删除的程序，
// 用户却看不到任何提示。
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  ResultCode: Integer;
begin
  if CurUninstallStep <> usUninstall then
    Exit;

  if Exec(ExpandConstant('{sys}\schtasks.exe'), '/delete /tn VRTX /f', '',
      SW_HIDE, ewWaitUntilTerminated, ResultCode) and (ResultCode = 0) then
    Exit;

  // 删除失败不等于任务还在：任务本来就不存在时 schtasks 同样返回非零。
  // 因此再查一次，只有任务确实存在时才提示，否则会把“从没开过自启动”
  // 误报成删除失败。
  if (not Exec(ExpandConstant('{sys}\schtasks.exe'), '/query /tn VRTX', '',
      SW_HIDE, ewWaitUntilTerminated, ResultCode)) or (ResultCode <> 0) then
    Exit;

  MsgBox('未能自动删除开机自启动任务 VRTX，通常是因为权限不足。' + #13#10 +
         '请在“任务计划程序”中手动删除名为 VRTX 的任务，' + #13#10 +
         '否则每次开机都会尝试运行一个已经被删除的程序。',
         mbInformation, MB_OK);
end;
