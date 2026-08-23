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

[Languages]
Name: "chinese"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"
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
    Flags: nowait postinstall skipifsilent

[UninstallDelete]
; 运行期产生的临时解包目录（如存在）；vrtx.json 按便携模型刻意保留，不在清理范围
Type: filesandordirs; Name: "{localappdata}\Temp\vrtx-vscdb-*.tmp"
