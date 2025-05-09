package winapi

import (
	"fmt"
	"log"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                      = syscall.NewLazyDLL("user32.dll")
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procSendMessage             = user32.NewProc("SendMessageW")
	procSendInput               = user32.NewProc("SendInput")
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

type INPUT struct {
	Type uint32
	Mi   MOUSEINPUT
}

var internalMonitorIsOff bool = false // 程序启动时，假定显示器是开启的

func simulateMouseMove() {
	var input [1]INPUT
	input[0].Type = INPUT_MOUSE
	input[0].Mi.Dx = 1 // 实际移动1个像素
	input[0].Mi.Dy = 1 // 轻微对角线移动
	input[0].Mi.MouseData = 0
	input[0].Mi.DwFlags = MOUSEEVENTF_MOVE
	input[0].Mi.Time = 0
	input[0].Mi.DwExtraInfo = 0

	_, _, err := procSendInput.Call(uintptr(1), uintptr(unsafe.Pointer(&input[0])), uintptr(unsafe.Sizeof(input[0])))
	if err != syscall.Errno(0) { // syscall.Errno(0) 表示成功
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
