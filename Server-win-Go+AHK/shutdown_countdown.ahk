; === 配置区 ===
; #SingleInstance force ; <--- 已移除, Go 程序将控制实例
#Persistent
SetBatchLines, -1
CoordMode, ToolTip, Screen

; -- 用户可配置变量 --
countdownSeconds := 5
interval := 16
BackgroundColor := "333333"
progressBarColor := "FF0000"

; -- 内部变量 --
totalMilli := countdownSeconds * 1000
tick := 0
lastPercent := -1
lastSecond := -1
WM_LBUTTONDOWN := 0x201

; --- 窗口标题 (可以简单，因为Go不通过标题来管理它了) ---
ShutdownCountdownWindowTitle := "Bealink Shutdown Countdown"

; === GUI 创建 ===
Gui, Color, %BackgroundColor%
Gui, +AlwaysOnTop -Caption +ToolWindow +Border
Gui, Margin, 55, 35
Gui, Font, s18, Segoe UI
Gui, Add, Text, vCountdownText w280 Center cffffff
Gui, Font, s12, Segoe UI
Gui, Add, Text, w280 Center cffffff, 点击窗口内任意区域取消
Gui, Font, s12, Segoe UI
Gui, Add, Progress, vProgressBar w280 h20 c%progressBarColor% Range0-100 yp+80

Gui, Show,, %ShutdownCountdownWindowTitle%

OnMessage(WM_LBUTTONDOWN, "ClickToCancel") ; 用户仍然可以点击窗口取消
SetTimer, UpdateCountdown, %interval%
Return

UpdateCountdown:
  tick += interval
  if (tick >= totalMilli)
  {
    GuiControl,, ProgressBar, 100
    GuiControl,, CountdownText, 将在 0 秒后准备关机...
    Sleep, 100
    SetTimer, UpdateCountdown, Off
    Gui, Destroy
    Shutdown, 1 ; 执行关机命令
    ExitApp
  }
  percent := Round(tick / totalMilli * 100)
  secondsLeft := Ceil((totalMilli - tick) / 1000)
  if (percent != lastPercent)
  {
    GuiControl,, ProgressBar, %percent%
    lastPercent := percent
  }
  if (secondsLeft != lastSecond)
  {
    GuiControl,, CountdownText, 将在 %secondsLeft% 秒后准备关机...
    lastSecond := secondsLeft
  }
Return

; === 事件处理函数 (标签/函数) ===
ClickToCancel() {
  global
  Gosub, CancelActions
}
GuiEscape: ; 用户按 ESC 也可以取消
GuiClose:  ; 用户点关闭按钮 (虽然没有) 也可以取消
CancelActions:
  SetTimer, UpdateCountdown, Off
  Gui, Destroy
  ExitApp ; 脚本自行退出
Return
