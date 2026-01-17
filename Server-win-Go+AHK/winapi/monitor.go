package winapi

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                      = syscall.NewLazyDLL("user32.dll")
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procSendMessage             = user32.NewProc("SendMessageW")
	procSendInput               = user32.NewProc("SendInput")
	procKeybdEvent              = user32.NewProc("keybd_event")
	procOpenClipboard           = user32.NewProc("OpenClipboard")
	procEmptyClipboard          = user32.NewProc("EmptyClipboard")
	procSetClipboardData        = user32.NewProc("SetClipboardData")
	procCloseClipboard          = user32.NewProc("CloseClipboard")
	procGlobalAlloc             = kernel32.NewProc("GlobalAlloc")
	procGlobalLock              = kernel32.NewProc("GlobalLock")
	procGlobalUnlock            = kernel32.NewProc("GlobalUnlock")
	procSetThreadExecutionState = kernel32.NewProc("SetThreadExecutionState")
)

const (
	WM_SYSCOMMAND   = 0x0112
	SC_MONITORPOWER = 0xF170
	// MONITOR_ON      = -1 // 不再直接使用 -1 常量进行转换
	MONITOR_OFF    = 2 // 关闭显示器的参数 (int 类型常量)
	HWND_BROADCAST = uintptr(0xffff)

	ERROR_ACCESS_DENIED syscall.Errno = 5 // syscall.Errno 类型

	INPUT_MOUSE      = 0
	MOUSEEVENTF_MOVE = 0x0001

	ES_CONTINUOUS       = 0x80000000
	ES_SYSTEM_REQUIRED  = 0x00000001
	ES_DISPLAY_REQUIRED = 0x00000002
)

type MOUSEINPUT struct {
	Dx          int32
	Dy          int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type KEYBDINPUT struct {
	WVk         uint16
	WScan       uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type INPUT struct {
	Type uint32
	Data [28]byte
}

type FILEDROP struct {
	PtX  uint32 // POINT.x
	PtY  uint32 // POINT.y
	Wide uint32 // TRUE if wide characters (Unicode)
}

const (
	INPUT_KEYBOARD  = 1
	KEYEVENTF_KEYUP = 0x0002

	VK_VOLUME_UP        = 0xAF
	VK_VOLUME_DOWN      = 0xAE
	VK_MEDIA_PLAY_PAUSE = 0xB3
	VK_MEDIA_NEXT_TRACK = 0xB0
	VK_MEDIA_PREV_TRACK = 0xB1
	VK_CONTROL          = 0x11
	VK_V                = 0x56
	VK_MENU             = 0x12
	VK_SHIFT            = 0x10
	VK_LEFT             = 0x25
	VK_RIGHT            = 0x27

	CF_TEXT     = 1
	CF_BITMAP   = 2
	CF_DIB      = 8
	CF_FILEDROP = 15
	GHND        = 0x0042
)

var internalMonitorIsOff bool = false // 程序启动时，假定显示器是开启的

func simulateMouseMove() {
	var input [1]INPUT
	var mi MOUSEINPUT
	input[0].Type = INPUT_MOUSE
	mi.Dx = 1
	mi.Dy = 1
	mi.MouseData = 0
	mi.DwFlags = MOUSEEVENTF_MOVE
	mi.Time = 0
	mi.DwExtraInfo = 0
	copy(input[0].Data[:], (*[28]byte)(unsafe.Pointer(&mi))[:])

	_, _, err := procSendInput.Call(uintptr(1), uintptr(unsafe.Pointer(&input[0])), uintptr(unsafe.Sizeof(input[0])))
	if err != syscall.Errno(0) {
		log.Printf("警告: 模拟鼠标移动以唤醒显示器失败: %v", err)
	} else {
		log.Println("信息: 已发送模拟鼠标移动事件以唤醒显示器。")
	}
}

func preventSleepAndWakeDisplay() {
	_, _, err := procSetThreadExecutionState.Call(ES_CONTINUOUS | ES_DISPLAY_REQUIRED | ES_SYSTEM_REQUIRED)
	if err != syscall.Errno(0) { // syscall.Errno(0) 表示成功
		log.Printf("警告: SetThreadExecutionState (ES_DISPLAY_REQUIRED) 调用失败: %v", err)
	} else {
		log.Println("信息: SetThreadExecutionState (ES_DISPLAY_REQUIRED) 调用成功。")
	}
}

// ToggleMonitorPower 切换显示器的电源状态（开/关）。
func ToggleMonitorPower() (newStateIsOff bool, err error) {
	var actionLParam uintptr // 用于 SendMessage 的 LPARAM 参数
	var actionMessage string
	var attemptToWake bool = false

	if internalMonitorIsOff { // 当前推测是关闭的，目标是开启
		actionMessage = "开启显示器"
		attemptToWake = true
		actionLParam = ^uintptr(0) // 用户建议的正确方式，等同于 MONITOR_ON (-1) 的位模式
	} else { // 当前推测是开启的，目标是关闭
		actionMessage = "关闭显示器"
		actionLParam = uintptr(MONITOR_OFF) // MONITOR_OFF 是 2
	}

	log.Printf("准备%s...", actionMessage)

	if attemptToWake {
		// 步骤1: 通知系统显示器是必需的
		preventSleepAndWakeDisplay()

		// 步骤2: 发送标准开启指令
		log.Printf("信息: 尝试发送 SC_MONITORPOWER (MONITOR_ON 参数: %X) 指令...", actionLParam)
		_, _, sendMsgErrOn := procSendMessage.Call(HWND_BROADCAST, WM_SYSCOMMAND, SC_MONITORPOWER, actionLParam)
		if sendMsgErrOn != syscall.Errno(0) {
			if sendMsgErrOn == ERROR_ACCESS_DENIED {
				log.Printf("警告: SendMessage (MONITOR_ON) 返回 'Access is denied'，这是已知情况。")
			} else {
				log.Printf("警告: SendMessage (MONITOR_ON) 调用失败: %v", sendMsgErrOn)
			}
		} else {
			log.Println("信息: SendMessage (MONITOR_ON) 指令已发送。")
		}

		time.Sleep(150 * time.Millisecond) // 给API一些时间反应

		// 步骤3: 执行核心唤醒操作 - 模拟鼠标移动
		simulateMouseMove()

		internalMonitorIsOff = false // 假设唤醒操作会成功
		log.Printf("%s序列已执行。服务端推测显示器新状态为: 开启", actionMessage)
		return internalMonitorIsOff, nil

	} else { // 关闭显示器的逻辑
		ret, _, apiErr := procSendMessage.Call(HWND_BROADCAST, WM_SYSCOMMAND, SC_MONITORPOWER, actionLParam)
		if apiErr != syscall.Errno(0) {
			if apiErr == ERROR_ACCESS_DENIED {
				log.Printf("警告: SendMessage (SC_MONITORPOWER - %s) API 调用返回 'Access is denied'。操作通常仍会生效。", actionMessage)
				internalMonitorIsOff = true // 即使有警告，也假设关闭成功
				log.Printf("%s命令已处理。服务端推测显示器新状态为: 关闭", actionMessage)
				return internalMonitorIsOff, nil
			}
			// 对于其他错误，保持当前推测状态不变，并返回错误
			return internalMonitorIsOff, fmt.Errorf("SendMessage (SC_MONITORPOWER - %s) API 调用失败 (err: %w, ret: %d)", actionMessage, apiErr, ret)
		}
		// SendMessage 调用成功 (无错误)
		internalMonitorIsOff = true
		log.Printf("%s命令已成功发送。服务端推测显示器新状态为: 关闭", actionMessage)
		return internalMonitorIsOff, nil
	}
}

// GetCurrentMonitorState 返回当前程序推测的显示器状态。
// true 表示关闭，false 表示开启。
func GetCurrentMonitorState() bool {
	return internalMonitorIsOff
}

// SendKeyPress 发送键盘按键 (使用 keybd_event)
func SendKeyPress(vk uint16) error {
	// 按下
	_, _, err := procKeybdEvent.Call(uintptr(vk), 0, 0, 0)
	if err != syscall.Errno(0) {
		return fmt.Errorf("发送键盘按下失败: %v", err)
	}
	// 松开
	_, _, err = procKeybdEvent.Call(uintptr(vk), 0, uintptr(2), 0) // KEYEVENTF_KEYUP = 2
	if err != syscall.Errno(0) {
		return fmt.Errorf("发送键盘松开失败: %v", err)
	}
	return nil
}

// VolumeUp 增加音量
func VolumeUp() error {
	return SendKeyPress(VK_VOLUME_UP)
}

// VolumeDown 减少音量
func VolumeDown() error {
	return SendKeyPress(VK_VOLUME_DOWN)
}

// MediaPlayPause 播放/暂停媒体
func MediaPlayPause() error {
	return SendKeyPress(VK_MEDIA_PLAY_PAUSE)
}

// MediaNext 下一首歌
func MediaNext() error {
	return SendKeyPress(VK_MEDIA_NEXT_TRACK)
}

// MediaPrev 上一首歌
func MediaPrev() error {
	return SendKeyPress(VK_MEDIA_PREV_TRACK)
}

// SetClipboardImage 设置图片到剪贴板（保存文件后使用 PowerShell 设置剪贴板）
func SetClipboardImage(data []byte, filename string) error {
	log.Printf("开始设置图片到剪贴板，数据大小: %d 字节，原文件名: %s", len(data), filename)

	if len(data) == 0 {
		return fmt.Errorf("图片数据为空")
	}

	// 步骤1: 将图片保存到临时目录
	log.Println("步骤1: 保存图片文件到临时目录...")
	tempDir := filepath.Join(os.TempDir(), "bealink_clipboard")
	err := os.MkdirAll(tempDir, 0755)
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %v", err)
	}
	log.Printf("临时目录已创建/确认: %s", tempDir)

	tempFilePath := filepath.Join(tempDir, filename)
	log.Printf("正在将图片保存到: %s", tempFilePath)

	err = os.WriteFile(tempFilePath, data, 0644)
	if err != nil {
		return fmt.Errorf("保存图片文件失败: %v", err)
	}
	log.Printf("图片文件已保存，文件大小: %d 字节", len(data))

	// 验证文件是否存在
	fileInfo, err := os.Stat(tempFilePath)
	if err != nil {
		return fmt.Errorf("验证文件失败: %v", err)
	}
	log.Printf("文件验证成功，文件大小: %d 字节", fileInfo.Size())

	// 步骤2: 使用 PowerShell 将文件设置到剪贴板
	log.Println("步骤2: 使用 PowerShell 设置文件到剪贴板...")
	err = SetFileToClipboardViaPowerShell(tempFilePath)
	if err != nil {
		return fmt.Errorf("设置剪贴板失败: %v", err)
	}

	log.Printf("图片文件已成功设置到剪贴板！文件路径: %s", tempFilePath)
	return nil
}

// SetFileToClipboardViaPowerShell 使用 PowerShell 将文件设置到 Windows 剪贴板
func SetFileToClipboardViaPowerShell(filePath string) error {
	log.Printf("使用 PowerShell 设置文件到剪贴板: %s", filePath)

	// 转义文件路径中的反斜杠和引号
	escapedPath := strings.ReplaceAll(filePath, "\\", "\\\\")
	escapedPath = strings.ReplaceAll(escapedPath, "\"", "\\\"")

	// 构造 PowerShell 脚本
	psScript := fmt.Sprintf(
		"Add-Type -AssemblyName System.Windows.Forms; "+
			"$files = New-Object System.Collections.Specialized.StringCollection; "+
			"$files.Add(\"%s\"); "+
			"[System.Windows.Forms.Clipboard]::SetFileDropList($files)",
		escapedPath,
	)

	log.Printf("PowerShell 脚本: %s", psScript)

	// 创建 PowerShell 命令，隐藏窗口
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", psScript)

	// Windows 特定：隐藏 PowerShell 窗口
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow: true,
		}
	}

	log.Println("正在执行 PowerShell 命令...")
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("PowerShell 执行失败: %v", err)
	}

	log.Printf("PowerShell 命令执行成功，文件已设置到剪贴板")
	return nil
}

// Paste 模拟粘贴操作 (Ctrl+V)
func Paste() error {
	// Ctrl 按下
	_, _, err := procKeybdEvent.Call(uintptr(VK_CONTROL), 0, 0, 0)
	if err != syscall.Errno(0) {
		return fmt.Errorf("发送 Ctrl 按下失败: %v", err)
	}
	// V 按下
	_, _, err = procKeybdEvent.Call(uintptr(VK_V), 0, 0, 0)
	if err != syscall.Errno(0) {
		return fmt.Errorf("发送 V 按下失败: %v", err)
	}
	// V 松开
	_, _, err = procKeybdEvent.Call(uintptr(VK_V), 0, uintptr(2), 0)
	if err != syscall.Errno(0) {
		return fmt.Errorf("发送 V 松开失败: %v", err)
	}
	// Ctrl 松开
	_, _, err = procKeybdEvent.Call(uintptr(VK_CONTROL), 0, uintptr(2), 0)
	if err != syscall.Errno(0) {
		return fmt.Errorf("发送 Ctrl 松开失败: %v", err)
	}
	return nil
}

// SendKeyWithModifiers 发送组合键（可包含 Ctrl / Alt / Shift）
func SendKeyWithModifiers(ctrl, alt, shift bool, vk uint16) error {
	// 按下修饰键
	if ctrl {
		_, _, err := procKeybdEvent.Call(uintptr(VK_CONTROL), 0, 0, 0)
		if err != syscall.Errno(0) {
			return fmt.Errorf("发送 Ctrl 按下失败: %v", err)
		}
	}
	if alt {
		_, _, err := procKeybdEvent.Call(uintptr(VK_MENU), 0, 0, 0)
		if err != syscall.Errno(0) {
			// 释放已按下的修饰键
			if ctrl {
				procKeybdEvent.Call(uintptr(VK_CONTROL), 0, uintptr(KEYEVENTF_KEYUP), 0)
			}
			return fmt.Errorf("发送 Alt 按下失败: %v", err)
		}
	}
	if shift {
		_, _, err := procKeybdEvent.Call(uintptr(VK_SHIFT), 0, 0, 0)
		if err != syscall.Errno(0) {
			if alt {
				procKeybdEvent.Call(uintptr(VK_MENU), 0, uintptr(KEYEVENTF_KEYUP), 0)
			}
			if ctrl {
				procKeybdEvent.Call(uintptr(VK_CONTROL), 0, uintptr(KEYEVENTF_KEYUP), 0)
			}
			return fmt.Errorf("发送 Shift 按下失败: %v", err)
		}
	}

	// 按下主键
	_, _, err := procKeybdEvent.Call(uintptr(vk), 0, 0, 0)
	if err != syscall.Errno(0) {
		// 释放修饰键
		if shift {
			procKeybdEvent.Call(uintptr(VK_SHIFT), 0, uintptr(KEYEVENTF_KEYUP), 0)
		}
		if alt {
			procKeybdEvent.Call(uintptr(VK_MENU), 0, uintptr(KEYEVENTF_KEYUP), 0)
		}
		if ctrl {
			procKeybdEvent.Call(uintptr(VK_CONTROL), 0, uintptr(KEYEVENTF_KEYUP), 0)
		}
		return fmt.Errorf("发送主键按下失败: %v", err)
	}
	// 松开主键
	_, _, err = procKeybdEvent.Call(uintptr(vk), 0, uintptr(KEYEVENTF_KEYUP), 0)
	if err != syscall.Errno(0) {
		// 仍继续尝试释放修饰键
	}

	// 释放修饰键（按相反顺序）
	if shift {
		procKeybdEvent.Call(uintptr(VK_SHIFT), 0, uintptr(KEYEVENTF_KEYUP), 0)
	}
	if alt {
		procKeybdEvent.Call(uintptr(VK_MENU), 0, uintptr(KEYEVENTF_KEYUP), 0)
	}
	if ctrl {
		procKeybdEvent.Call(uintptr(VK_CONTROL), 0, uintptr(KEYEVENTF_KEYUP), 0)
	}
	return nil
}
