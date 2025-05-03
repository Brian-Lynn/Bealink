; notify.ahk - 显示一个自定义样式的通知窗口
; 接收第一个命令行参数作为通知内容
; (根据用户提供的能正常运行的版本恢复)

#NoTrayIcon
#SingleInstance Force
SetTitleMatchMode, 2

; --- 配置 ---
WindowTitle := "Bealink 通知" ; 内部窗口标题，用于 WinMove 等操作
DisplaySeconds := 5       ; 通知显示秒数
MaxWidth := 350           ; 窗口最大宽度 (像素)
Padding := 15             ; 内边距
BackgroundColor := "333333" ; 深灰色背景
TextColor := "FFFFFF"     ; 白色文字
TitleText := "剪贴板已同步"   ; 通知标题
TitleFontSize := 11
ContentFontSize := 10
FontName := "Microsoft YaHei UI" ; 字体

; --- 获取命令行参数 ---
notificationContent = %1% ; 使用传统赋值方式获取第一个参数
if (notificationContent = "") {
    notificationContent := "(无通知内容)"
}

; --- GUI 定义 ---
Gui, Color, %BackgroundColor%
Gui, Font, s%TitleFontSize% Bold, %FontName%
; 添加标题文本，关联变量 vNotificationTitle
Gui, Add, Text, x%Padding% y%Padding% c%TextColor% BackgroundTrans vNotificationTitle, %TitleText%

Gui, Font, s%ContentFontSize% Normal, %FontName% ; 切换回内容字体
contentWidth := MaxWidth - (Padding * 2) ; 内容区域的最大宽度
estimatedTitleHeight := 20 ; 估算标题高度用于定位内容
contentY := Padding + estimatedTitleHeight + 5
; 添加内容文本，关联变量 vNotificationContent，并限制宽度，允许自动换行
Gui, Add, Text, x%Padding% y%contentY% w%contentWidth% c%TextColor% BackgroundTrans vNotificationContent, %notificationContent%

; --- 窗口样式设置 ---
; 设置窗口样式：总在最前, 无标题栏, 工具窗口, 无边框
Gui, +LastFound +AlwaysOnTop -Caption +ToolWindow -Border

; --- 显示窗口 (先 AutoSize 确定大小并显示) ---
; 先用 AutoSize 显示窗口，让 AHK 确定尺寸
Gui, Show, AutoSize, %WindowTitle%

; --- 获取窗口尺寸和屏幕尺寸 ---
; 获取刚刚显示的窗口的实际宽度和高度
WinGetPos,,, Width, Height, %WindowTitle%
; 获取屏幕工作区的宽度和高度 (78=宽度, 79=高度, 排除任务栏)
SysGet, MonitorWidth, 78
SysGet, MonitorHeight, 79

; --- 计算最终位置 ---
MarginX := 20 ; 右边距
MarginY := 30 ; 下边距
WinPosX := MonitorWidth - Width - MarginX   ; 使用获取到的实际宽度
WinPosY := MonitorHeight - Height - MarginY ; 使用获取到的实际高度

; --- 移动窗口到计算好的位置 ---
; 使用 WinMove 命令将已显示的窗口移动到右下角
WinMove, %WindowTitle%,, %WinPosX%, %WinPosY%

; --- 设置定时关闭 ---
; 设置定时器，在指定秒数后调用 CloseGui 标签 (负数表示只执行一次)
; 注意：AHK v1 定时器单位是毫秒
SetTimer, CloseGui, % DisplaySeconds * -1000

return ; 结束自动执行段

; --- 定时器处理 ---
CloseGui:
    Gui, Destroy ; 销毁窗口
    ExitApp      ; 退出脚本
return

; --- 窗口关闭处理 (以防用户通过其他方式关闭) ---
GuiClose:
    ExitApp
return
