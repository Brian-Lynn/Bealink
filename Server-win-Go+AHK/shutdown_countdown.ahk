; === 配置区 ===
; 脚本指令: 确保同一时间只有一个此脚本的实例运行
#SingleInstance force
; 脚本指令: 让脚本在后台持续运行
#Persistent
; 脚本指令: 让脚本以最高速度运行
SetBatchLines, -1
; 脚本指令: 设置 ToolTip 的坐标模式为相对于屏幕
CoordMode, ToolTip, Screen ; (虽然此脚本没用 ToolTip, 但保留用户添加的指令)

; -- 用户可配置变量 --
countdownSeconds := 5       ; 倒计时总秒数 (单位: 秒)
interval := 16              ; 定时器触发间隔 (单位: 毫秒, 16ms ≈ 60Hz)
BackgroundColor := "333333" ; 深灰色背景
progressBarColor := "FF0000" ; 进度条颜色 (红色 - Shutdown 版)

; -- 内部变量 --
totalMilli := countdownSeconds * 1000 ; 计算总毫秒数
tick := 0                   ; 用于累加经过的毫秒数
lastPercent := -1           ; 用于跟踪上次显示的进度百分比
lastSecond := -1            ; 用于跟踪上次显示的剩余秒数
WM_LBUTTONDOWN := 0x201     ; 定义鼠标左键按下的消息编号
; WM_KEYDOWN 已移除

; === GUI (图形用户界面) 创建 ===
; 设置窗口样式: 总在最前, 无标题栏, 工具窗口样式, 添加边框
Gui, Color, %BackgroundColor%
Gui, +AlwaysOnTop -Caption +ToolWindow +Border
; 设置 GUI 控件的边距 (应用个性化设置)
Gui, Margin, 55, 35
; 设置 GUI 控件使用的字体: 大小 18 (应用个性化设置), 字体 "Segoe UI"
Gui, Font, s18, Segoe UI
; 添加一个文本控件: 关联变量 vCountdownText, 宽度 280, 居中对齐
Gui, Add, Text, vCountdownText w280 Center cffffff
; 设置较小的字体用于提示文字 (应用个性化设置)
Gui, Font, s12, Segoe UI
; 添加提示文字: 宽度 280, 居中对齐
Gui, Add, Text, w280 Center cffffff, 点击窗口内任意区域取消
; 恢复默认字体大小，用于后续控件 (恢复到 s12 以防万一)
Gui, Font, s12, Segoe UI
; 添加一个进度条控件: 关联变量 vProgressBar, 宽度 280, 高度 20, 颜色由变量指定, 范围 0-100, 向下移动更多留出空间 (应用个性化设置 yp+80)
Gui, Add, Progress, vProgressBar w280 h20 c%progressBarColor% Range0-100 yp+80
; --- 取消按钮已被移除 ---
; 显示 GUI 窗口: 窗口标题 "倒计时关机提示" (窗口大小会自动调整)
Gui, Show,, 倒计时关机提示 ; <--- 修改窗口标题

; 使用 OnMessage 监视鼠标左键点击窗口事件，并将其导向 ClickToCancel 函数/标签
OnMessage(WM_LBUTTONDOWN, "ClickToCancel")
; OnMessage(WM_KEYDOWN, ...) 已移除

; 启动定时器: 每隔 interval 毫秒，执行一次 UpdateCountdown 标签处的代码
SetTimer, UpdateCountdown, %interval%
; 结束脚本的自动执行段，等待事件
Return

; === 定时器处理函数 (标签) ===
UpdateCountdown:
  ; 累加经过的毫秒数
  tick += interval

  ; 检查是否到达总时间
  if (tick >= totalMilli) ; 使用 >= 更严谨
  {
    ; 确保进度条显示为 100%
    GuiControl,, ProgressBar, 100
    ; 确保文本显示为 0 秒 (使用关机文本)
    GuiControl,, CountdownText, 将在 0 秒后准备关机... ; <--- 修改文本
    ; 短暂暂停，让用户看到最终状态 (可选)
    Sleep, 100
    ; 关闭定时器
    SetTimer, UpdateCountdown, Off
    ; 销毁 GUI 窗口
    Gui, Destroy
    ; 执行关机命令 (Shutdown, 1 表示关机)
    Shutdown, 1 ; <--- 修改为关机命令
    ; 退出脚本
    ExitApp
  }

  ; 计算当前进度百分比
  percent := Round(tick / totalMilli * 100)
  ; 计算剩余秒数 (向上取整)
  secondsLeft := Ceil((totalMilli - tick) / 1000)

  ; 只在进度百分比变动时更新进度条 GUI，避免不必要的刷新
  if (percent != lastPercent)
  {
    GuiControl,, ProgressBar, %percent%
    lastPercent := percent ; 更新最后显示的百分比
  }

  ; 只在剩余秒数变动时更新文本 GUI (使用关机文本)
  if (secondsLeft != lastSecond)
  {
    GuiControl,, CountdownText, 将在 %secondsLeft% 秒后准备关机... ; <--- 修改文本
    lastSecond := secondsLeft ; 更新最后显示的秒数
  }
Return

; === 事件处理函数 (标签/函数) ===

; -- 当点击窗口背景时执行 (由 OnMessage 调用) --
ClickToCancel() {
  global ; 确保能访问全局变量和标签
  Gosub, CancelActions      ; 跳转到统一的取消处理逻辑
}

; KeyPressToCancel() 函数已移除

; -- 当按下 Esc 键时执行 (如果 GUI 窗口激活) --
GuiEscape:
; -- 当点击窗口的关闭按钮 (虽然隐藏了标题栏，但此标签可作为备用) 时执行 --
GuiClose:
CancelActions:              ; <--- 统一的取消处理标签
  ; 关闭定时器
  SetTimer, UpdateCountdown, Off
  ; 销毁 GUI 窗口
  Gui, Destroy
  ; 退出脚本程序
  ExitApp
Return