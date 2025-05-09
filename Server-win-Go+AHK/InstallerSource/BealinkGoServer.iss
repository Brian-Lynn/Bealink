[Setup]
AppName=BeaLink Server
AppVersion=1.1.45.14
; --- AppId 非常重要，用于卸载和版本管理 ---
AppId=BeaLinkServerApp114514
; 使用 {autopf} 自动选择 Program Files 目录 (例如 C:\Program Files 或 C:\Program Files (x86))
DefaultDirName={autopf}\BeaLink Server
; 开始菜单文件夹名
DefaultGroupName=BeaLink Server
UninstallDisplayIcon={app}\bealinkserver.exe
; 生成的安装包存放目录 (相对于 .iss 文件)
OutputDir=.\OutputInstaller
; 生成的安装包文件名，包含版本号以方便管理
OutputBaseFilename=BeaLink_Server_Installer_v1.1.45.14
Compression=lzma
SolidCompression=yes
; SetupIconFile 指向与 .iss 文件同目录的 icon.ico
SetupIconFile=icon.ico
; *** 新增 [Languages] 区段 ***

[Languages]
; 或者更保险的方式，如果 Inno Setup 的 Languages 目录已配置好：
Name: "chinesesimplified"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"



[Tasks]
; 开机自启任务，默认不勾选，让用户选择
Name: "autoStart"; Description: "开机时启动 BeaLink Server"; GroupDescription: "附加任务："; Flags: unchecked

[Files]
; --- 所有 Source 路径都已更新，假设源文件与 .iss 文件在同一目录 ---
; 确保 bealinkserver.exe 是使用 go build -ldflags "-H windowsgui" 编译的，以避免运行时出现命令行窗口
Source: "bealinkserver.exe"; DestDir: "{app}"; Flags: ignoreversion
; BonjourSDKSetup.exe 存放在临时目录，并在安装后删除
Source: "BonjourSDKSetup.exe"; DestDir: "{tmp}"; Flags: deleteafterinstall
; AutoHotkey.exe 将被复制到应用程序目录
Source: "AutoHotkey.exe"; DestDir: "{app}"; Flags: ignoreversion
; icon.ico 将被复制到应用程序目录
Source: "icon.ico"; DestDir: "{app}"; Flags: ignoreversion
; AHK 脚本将被复制到应用程序目录
Source: "notify.ahk"; DestDir: "{app}"; Flags: ignoreversion
Source: "shutdown_countdown.ahk"; DestDir: "{app}"; Flags: ignoreversion
Source: "sleep_countdown.ahk"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
; 在开始菜单中创建程序快捷方式
Name: "{group}\BeaLink Server"; Filename: "{app}\bealinkserver.exe"; WorkingDir: "{app}"
; 在开始菜单中创建卸载快捷方式
Name: "{group}\卸载 BeaLink Server"; Filename: "{uninstallexe}"

[Run]
; 安装 Bonjour (如果 NeedsBonjour 函数返回 true)
Filename: "{tmp}\BonjourSDKSetup.exe"; Parameters: "/quiet"; StatusMsg: "正在安装 Bonjour 服务..."; Check: NeedsBonjour

; 安装后运行主程序 (可选，静默安装时跳过)
Filename: "{app}\bealinkserver.exe"; Description: "运行 BeaLink Server"; Flags: nowait postinstall skipifsilent

[Registry]
; 开机自启注册表项
; Root: HKCU (HKEY_CURRENT_USER)
; Subkey: Software\Microsoft\Windows\CurrentVersion\Run
; ValueName: 确保与 Go 代码中的 winapi.registryValueName (即 "BealinkGoServer") 一致
; ValueType: string (REG_SZ)
; ValueData: 程序的完整路径，用引号括起来以处理路径中的空格
; Tasks: autoStart 表示只有当用户勾选了名为 "autoStart" 的任务时，才执行此条注册表写入
; Flags: uninsdeletevalue 表示卸载时删除此注册表值 (虽然下面 [UninstallRegistry] 中有更明确的删除)
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueName: "BealinkGoServer"; ValueType: string; ValueData: """{app}\bealinkserver.exe"""; Tasks: autoStart; Flags: uninsdeletevalue

[UninstallDelete]
; 卸载时删除整个应用程序安装目录及其所有内容
Type: filesandordirs; Name: "{app}"

[UninstallRegistry]
; 卸载时明确删除开机自启的注册表项，无论安装时是否勾选创建
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueName: "BealinkGoServer"; Flags: deletevalue

[UninstallRun]
; 卸载前尝试结束正在运行的程序进程，防止文件占用导致卸载失败
; taskkill.exe /F (强制) /IM (按镜像名)
Filename: "taskkill.exe"; Parameters: "/F /IM bealinkserver.exe"; Flags: runhidden waituntilterminated

[Code]
// Pascal Script 部分

// --- 使用预处理指令定义 AppId，方便在脚本中引用 ---
#define MyAppId "BeaLinkServerApp114514"

// InitializeSetup 在安装开始时运行，用于检测旧版本
function InitializeSetup(): Boolean;
var
  UninstPath: String; // 用于存储查找到的卸载路径字符串 (虽然在此函数中未直接使用其值)
begin
  // 检查注册表中是否有基于 AppId 的已安装记录
  // Inno Setup 会为卸载信息创建一个键，通常是 AppId + "_is1"
  if RegQueryStringValue(HKEY_LOCAL_MACHINE, 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{#MyAppId}_is1', 'UninstallString', UninstPath) or
     RegQueryStringValue(HKEY_CURRENT_USER, 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{#MyAppId}_is1', 'UninstallString', UninstPath) then
  begin
    // 如果检测到已安装，提示用户是否覆盖
    if MsgBox('检测到 BeaLink Server 已安装。是否要继续安装并覆盖现有版本？', mbConfirmation, MB_YESNO) = IDNO then
      Result := False // 用户选择“否”，则不继续安装
    else
      Result := True; // 用户选择“是”，继续安装 (覆盖)
  end
  else
  begin
    // 没有检测到已安装的版本，允许正常安装
    Result := True;
  end;
end;

// NeedsBonjour 检测 Bonjour 服务是否存在
function NeedsBonjour: Boolean;
begin
  // 检查 Bonjour 服务的注册表项是否存在
  Result := not RegKeyExists(HKEY_LOCAL_MACHINE, 'SYSTEM\CurrentControlSet\Services\Bonjour Service');
end;

