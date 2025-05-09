; === 配置区 ===
; #SingleInstance force ; <--- 已移除, Go 程序将控制实例
#Persistent
SetBatchLines, -1
CoordMode, ToolTip, Screen

; -- 用户可配置变量 --
countdownSeconds := 5
interval := 16
borderColor := "0078D7"
BackgroundColor := "333333"

; -- 内部变量 --
totalMilli := countdownSeconds * 1000
tick := 0
lastPercent := -1
lastSecond := -1
WM_LBUTTONDOWN := 0x201

; --- 窗口标题 ---
SleepCountdownWindowTitle := "Bealink Sleep Countdown"

; === GUI 创建 ===
Gui, Color, %BackgroundColor%
Gui, +AlwaysOnTop -Caption +ToolWindow +Border
Gui, Margin, 55, 35
Gui, Font, s18 , Segoe UI
Gui, Add, Text, vCountdownText w280 Center cffffff
Gui, Font, s12, Segoe UI
Gui, Add, Text, w280 Center cffffff, 点击窗口内任意区域取消
Gui, Font, s12, Segoe UI
Gui, Add, Progress, vProgressBar w280 h20 c%borderColor% Range0-100 yp+80

Gui, Show,, %SleepCountdownWindowTitle%

OnMessage(WM_LBUTTONDOWN, "ClickToCancel") ; 用户仍然可以点击窗口取消
SetTimer, UpdateCountdown, %interval%
Return

UpdateCountdown:
  tick += interval
  if (tick >= totalMilli)
  {
    GuiControl,, ProgressBar, 100
    GuiControl,, CountdownText, 将在 0 秒后准备睡眠...
    Sleep, 100
    SetTimer, UpdateCountdown, Off
    Gui, Destroy
    Run, rundll32.exe powrprof.dll`,SetSuspendState 0`,1`,0 ; 执行睡眠命令
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
    GuiControl,, CountdownText, 将在 %secondsLeft% 秒后准备睡眠...
    lastSecond := secondsLeft
  }
Return

ClickToCancel() {
  global
  Gosub, CancelActions
}
GuiEscape:
GuiClose:
CancelActions:
  SetTimer, UpdateCountdown, Off
  Gui, Destroy
  ExitApp ; 脚本自行退出
Return
