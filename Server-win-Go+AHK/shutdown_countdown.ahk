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
  tick += interval  ; 累加经过的时间间隔 [cite: 5]
  
  ; --- 关机逻辑 ---
  if (tick >= totalMilli) ; 如果经过的时间大于等于总倒计时时间 [cite: 5]
  {
    GuiControl,, ProgressBar, 100  ; 进度条设置为 100%
    GuiControl,, CountdownText, 将在 0 秒后准备关机... ; 更改倒计时文本
    Sleep, 100  ; 稍微等待一下，让用户看到最终状态
    SetTimer, UpdateCountdown, Off ; 停止计时器
    Gui, Destroy  ; 销毁 GUI 窗口
    
    Run, shutdown.exe /s /f /t 0  ; 尝试使用 shutdown.exe 强制关机
    if ErrorLevel != 0  ; 检查 shutdown.exe 是否执行失败 (ErrorLevel 通常为 0 表示成功)
    {
      Shutdown, 1  ; 如果 shutdown.exe 失败，则使用 AHK 的 Shutdown 命令作为备选
      ;  重要提示：ErrorLevel 的具体值需要根据你的 AHK 版本和系统环境测试确定！
      ;  请查阅 AHK 文档确认 shutdown.exe 失败时 ErrorLevel 的返回值。
    }
    
    ExitApp  ; 退出 AHK 脚本
  }
  ; --- 倒计时显示更新 ---
  percent := Round(tick / totalMilli * 100) ; 计算进度百分比 [cite: 6]
  secondsLeft := Ceil((totalMilli - tick) / 1000) ; 计算剩余秒数 [cite: 6]
  
  if (percent != lastPercent) ; 如果百分比发生变化 [cite: 6]
  {
    GuiControl,, ProgressBar, %percent% ; 更新进度条
    lastPercent := percent ; 更新 lastPercent，避免重复更新
  }
  
  if (secondsLeft != lastSecond) ; 如果剩余秒数发生变化 [cite: 6]
  {
    GuiControl,, CountdownText, 将在 %secondsLeft% 秒后准备关机... ; 更新倒计时文本
    lastSecond := secondsLeft ; 更新 lastSecond，避免重复更新
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
