// ************************************************************************
// ** 文件: main.go (UI和逻辑优化)                                        **
// ** 描述: 集成新的配置方式，修改托盘菜单，适配 Bark 通知事件。             **
// ** 主要改动：                                                     **
// ** - 开机和唤醒时都调用 bark.NotifyEvent("system_ready")。        **
// ************************************************************************
package main

import (
	"context"
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

	"bealinkserver/bark"
	"bealinkserver/logging"
	"bealinkserver/server"
	"bealinkserver/winapi"

	"github.com/getlantern/systray"
)

const (
	lightIconFileName      = "assets/light.ico"
	darkIconFileName       = "assets/dark.ico"
	WM_POWERBROADCAST      = 0x0218
	PBT_APMRESUMEAUTOMATIC = 0x0012
	PBT_APMRESUMESUSPEND   = 0x0007
	PBT_APMRESUMECRITICAL  = 0x0006
	WM_DESTROY_VALUE       = 0x0002
	WM_NULL                = 0x0000
)

var (
	actualServerAddr string
	powerEventHWND   syscall.Handle
)

var (
	user32DLL            = syscall.NewLazyDLL("user32.dll")
	kernel32DLL          = syscall.NewLazyDLL("kernel32.dll")
	procRegisterClassExW = user32DLL.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32DLL.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32DLL.NewProc("DefWindowProcW")
	procGetMessageW      = user32DLL.NewProc("GetMessageW")
	procTranslateMessage = user32DLL.NewProc("TranslateMessage")
	procDispatchMessageW = user32DLL.NewProc("DispatchMessageW")
	procDestroyWindow    = user32DLL.NewProc("DestroyWindow")
	procUnregisterClassW = user32DLL.NewProc("UnregisterClassW")
	procPostMessageW     = user32DLL.NewProc("PostMessageW")
)

func PostMessage(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) error {
	r1, _, e1 := procPostMessageW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	if r1 == 0 {
		if e1 != nil {
			return e1
		}
		return fmt.Errorf("PostMessageW 调用失败但无明确错误")
	}
	return nil
}

const powerEventWindowClassName = "BealinkPowerEventWnd"

type WNDCLASSEX struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	ClsExtra      int32
	WndExtra      int32
	HInstance     syscall.Handle
	HIcon         syscall.Handle
	HCursor       syscall.Handle
	HbrBackground syscall.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       syscall.Handle
}
type MSG struct {
	Hwnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

func powerEventWindowProc(hwnd syscall.Handle, msg uint32, wParam uintptr, lParam uintptr) uintptr {
	switch msg {
	case WM_POWERBROADCAST:
		if wParam == PBT_APMRESUMEAUTOMATIC || wParam == PBT_APMRESUMESUSPEND || wParam == PBT_APMRESUMECRITICAL {
			log.Println("检测到系统从睡眠/休眠状态唤醒。")
			go bark.NotifyEvent("system_ready") // <--- 统一事件名
		}
		return 0
	case WM_DESTROY_VALUE:
		log.Println("电源事件监听窗口已销毁 (WM_DESTROY)。")
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

func createPowerEventWindow(hInstance syscall.Handle) (syscall.Handle, error) {
	classNamePtr, _ := syscall.UTF16PtrFromString(powerEventWindowClassName)
	windowTitlePtr, _ := syscall.UTF16PtrFromString("Bealink Power Listener")
	wc := WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), LpfnWndProc: syscall.NewCallback(powerEventWindowProc), HInstance: hInstance, LpszClassName: classNamePtr}
	atom, _, errReg := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 && (errReg == nil || errReg.(syscall.Errno) != 1410) {
		return 0, fmt.Errorf("注册电源事件窗口类失败: %v (atom: %d)", errReg, atom)
	}
	if atom == 0 && errReg != nil && errReg.(syscall.Errno) == 1410 {
		log.Println("信息: 电源事件窗口类已注册。")
	}
	hwnd, _, errCreate := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(classNamePtr)), uintptr(unsafe.Pointer(windowTitlePtr)), 0, 0, 0, 0, 0, 0, 0, uintptr(hInstance), 0)
	if hwnd == 0 {
		return 0, fmt.Errorf("创建电源事件窗口失败: %v", errCreate)
	}
	log.Printf("电源事件监听窗口创建成功 (HWND: 0x%X)。", hwnd)
	return syscall.Handle(hwnd), nil
}

func powerEventMessageLoop(ctx context.Context, hInstance syscall.Handle) { /* ... (代码同前，确保 PostMessage 使用 WM_NULL) ... */
	log.Println("启动电源事件消息循环...")
	var errLoop error
	powerEventHWND, errLoop = createPowerEventWindow(hInstance)
	if errLoop != nil {
		log.Printf("!!! 致命错误: 无法创建电源事件监听窗口: %v。", errLoop)
		return
	}
	defer func() {
		log.Println("开始清理电源事件消息循环资源...")
		if powerEventHWND != 0 {
			log.Printf("正在销毁电源事件窗口 (HWND: 0x%X)...", powerEventHWND)
			if ret, _, destroyErr := procDestroyWindow.Call(uintptr(powerEventHWND)); ret == 0 {
				log.Printf("错误: 销毁电源事件窗口失败: %v", destroyErr)
			} else {
				log.Println("电源事件窗口已成功请求销毁。")
			}
			powerEventHWND = 0
		}
		classNamePtr, _ := syscall.UTF16PtrFromString(powerEventWindowClassName)
		if retUnregister, _, unregisterErr := procUnregisterClassW.Call(uintptr(unsafe.Pointer(classNamePtr)), uintptr(hInstance)); retUnregister == 0 {
			log.Printf("警告: 注销电源事件窗口类失败: %v", unregisterErr)
		} else {
			log.Println("电源事件窗口类已注销。")
		}
		log.Println("电源事件消息循环资源清理完毕。")
	}()
	var msg MSG
	running := true
	for running {
		select {
		case <-ctx.Done():
			log.Println("收到上下文取消信号，停止电源事件消息循环...")
			if powerEventHWND != 0 {
				PostMessage(powerEventHWND, WM_NULL, 0, 0)
			} // 使用我们定义的 WM_NULL
			running = false
		default:
			ret, _, getMsgErr := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if !running {
				log.Println("GetMessageW 返回，但循环已被指示停止。")
				break
			}
			if int32(ret) == 0 {
				log.Println("GetMessageW 返回 0，电源事件消息循环将退出。")
				running = false
				break
			}
			if int32(ret) == -1 {
				log.Printf("错误: GetMessageW 返回 -1: %v", getMsgErr)
				running = false
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}
	log.Println("电源事件消息循环已结束。")
}

func loadIconBytes(fileName string) []byte {
	exePath, _ := os.Executable()
	iconPath := filepath.Join(filepath.Dir(exePath), fileName)
	iconBytes, err := os.ReadFile(iconPath)
	if err != nil {
		log.Printf("警告: 加载图标 %s 失败: %v", iconPath, err)
		return nil
	}
	return iconBytes
}

// getIconFileName 根据系统主题返回对应的图标文件名
func getIconFileName() string {
	if runtime.GOOS != "windows" {
		// 非 Windows 系统默认使用浅色图标
		return lightIconFileName
	}
	isDark, err := winapi.IsDarkMode()
	if err != nil {
		log.Printf("警告: 检测系统主题失败: %v，使用默认浅色图标", err)
		return lightIconFileName
	}
	if isDark {
		return darkIconFileName
	}
	return lightIconFileName
}

// updateTrayIcon 根据系统主题更新托盘图标
func updateTrayIcon() {
	iconFileName := getIconFileName()
	iconBytes := loadIconBytes(iconFileName)
	if iconBytes != nil {
		systray.SetIcon(iconBytes)
		log.Printf("托盘图标已更新: %s", iconFileName)
	} else {
		log.Printf("警告: 无法加载图标文件: %s", iconFileName)
	}
}
func openBrowser(url string) error { /* ... (代码同前) ... */
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}
	log.Printf("尝试在浏览器中打开: %s", url)
	return cmd.Start()
}

func ensureSingleInstance() bool {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW := kernel32.NewProc("CreateMutexW")

	// 使用更具体的互斥体名称并添加错误检查
	mutexName, err := syscall.UTF16PtrFromString("Global\\BealinkGoServer_SingleInstance_Mutex_v1")
	if err != nil {
		log.Printf("错误: 创建互斥体名称失败: %v", err)
		return false
	}

	// 创建互斥体并立即获取错误代码
	h, _, errNo := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(mutexName)))
	lastErr := errNo.(syscall.Errno)

	if h == 0 {
		log.Printf("错误: 创建互斥体失败: %v", lastErr)
		return false
	}

	// 如果互斥体已存在，则查找主窗口并激活
	if lastErr == syscall.ERROR_ALREADY_EXISTS {
		go func() {
			// 等待一小段时间确保消息框不会太快显示
			time.Sleep(100 * time.Millisecond)
			findAndActivateMainWindow()
		}()
		return false
	}

	// 保存句柄以便程序退出时清理
	runtime.SetFinalizer(&h, func(handle *uintptr) {
		syscall.CloseHandle(syscall.Handle(*handle))
	})

	return true
}

func main() {
	if !ensureSingleInstance() {
		// 直接在主线程中显示消息框，确保它能显示出来
		title, _ := syscall.UTF16PtrFromString("Bealink 提示")
		text, _ := syscall.UTF16PtrFromString("Bealink 服务已在运行中。\n请查看系统托盘区的图标。")
		user32 := syscall.NewLazyDLL("user32.dll")
		procMessageBoxW := user32.NewProc("MessageBoxW")
		const MB_OK = 0x00000000
		const MB_ICONINFORMATION = 0x00000040
		const MB_SYSTEMMODAL = 0x00001000
		const MB_SETFOREGROUND = 0x00010000
		const MB_TOPMOST = 0x00040000
		procMessageBoxW.Call(0,
			uintptr(unsafe.Pointer(text)),
			uintptr(unsafe.Pointer(title)),
			uintptr(MB_OK|MB_ICONINFORMATION|MB_SYSTEMMODAL|MB_SETFOREGROUND|MB_TOPMOST))
		return
	}
	logWriter := logging.Init(200)
	log.SetOutput(logWriter)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("程序启动...")
	bark.InitConfig()
	_ = bark.GetNotifier()
	systray.Run(onReady, onExit)
}

func onReady() {
	log.Println("系统托盘准备就绪。")
	preferredPorts := []string{":8088", ":8089", ":8090", ":8080"}
	coreServiceCtx, coreServiceCancel := context.WithCancel(context.Background())
	go logging.GetHub().Run(coreServiceCtx)
	var errServerStart error
	var usedAlternativePort bool
	actualServerAddr, usedAlternativePort, errServerStart = server.Start(coreServiceCtx, preferredPorts, logging.GetHub())
	if errServerStart != nil {
		log.Fatalf("!!! 致命错误: HTTP服务启动失败: %v。", errServerStart)
		updateTrayIcon()
		systray.SetTitle("Bealink Go - 错误")
		systray.SetTooltip(fmt.Sprintf("服务启动失败: %v", errServerStart))
		mErrItem := systray.AddMenuItem(fmt.Sprintf("错误: %v", errServerStart), "服务无法启动")
		mErrItem.Disable()
		mQuitErr := systray.AddMenuItem("退出", "关闭程序")
		go func() { <-mQuitErr.ClickedCh; coreServiceCancel(); systray.Quit() }()
		return
	}
	if usedAlternativePort {
		log.Printf("服务已在备用地址 %s 上启动。", actualServerAddr)
	}
	updateTrayIcon()
	systray.SetTitle("Bealink Go 服务")
	systray.SetTooltip(fmt.Sprintf("Bealink Go (监听于 %s)", actualServerAddr))
	mSettings := systray.AddMenuItem("设置", "打开程序设置页面")
	mAutoStart := systray.AddMenuItem("开机自启", "设置/取消开机自启")
	autoStartEnabled, errAS := winapi.IsAutoStartEnabled()
	if errAS == nil && autoStartEnabled {
		mAutoStart.Check()
	} else if errAS != nil {
		log.Printf("错误: 检查开机自启状态失败: %v", errAS)
		mAutoStart.Disable()
	}
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "关闭服务")

	go func() { time.Sleep(2 * time.Second); bark.NotifyEvent("system_ready") }() // <--- 统一事件名

	// 启动主题监听，定期检查系统主题变化并更新图标
	if runtime.GOOS == "windows" {
		go func() {
			ticker := time.NewTicker(5 * time.Second) // 每5秒检查一次主题
			defer ticker.Stop()
			lastTheme := getIconFileName()
			for {
				select {
				case <-ticker.C:
					currentTheme := getIconFileName()
					if currentTheme != lastTheme {
						log.Println("检测到系统主题变化，更新托盘图标...")
						updateTrayIcon()
						lastTheme = currentTheme
					}
				case <-coreServiceCtx.Done():
					return
				}
			}
		}()
	}

	if runtime.GOOS == "windows" {
		hInst, _, errHInst := kernel32DLL.NewProc("GetModuleHandleW").Call(0)
		if hInst == 0 || (errHInst != nil && errHInst.(syscall.Errno) != 0) {
			log.Printf("警告: GetModuleHandleW(nil) 失败 (err: %v, handle: %v)。睡眠唤醒通知将不可用。", errHInst, hInst)
		} else {
			go powerEventMessageLoop(coreServiceCtx, syscall.Handle(hInst))
		}
	} else {
		log.Println("信息: 非 Windows 系统，不启动电源事件监听。")
	}

	go func() {
		defer log.Println("托盘菜单事件处理循环已退出。")
		for {
			select {
			case <-mSettings.ClickedCh:
				if actualServerAddr == "" {
					log.Println("错误: 服务器地址未知，无法打开设置页面。")
					continue
				}
				hostParts := strings.Split(actualServerAddr, ":")
				portStr := hostParts[len(hostParts)-1]
				settingsURL := fmt.Sprintf("http://localhost:%s/setting", portStr)
				if err := openBrowser(settingsURL); err != nil {
					log.Printf("错误: 打开设置页面 (%s) 失败: %v", settingsURL, err)
				}
			case <-mAutoStart.ClickedCh:
				if mAutoStart.Checked() {
					if err := winapi.DisableAutoStart(); err == nil {
						mAutoStart.Uncheck()
						log.Println("开机自启已禁用。")
					} else {
						log.Printf("错误: 禁用开机自启失败: %v", err)
					}
				} else {
					if err := winapi.EnableAutoStart(); err == nil {
						mAutoStart.Check()
						log.Println("开机自启已启用。")
					} else {
						log.Printf("错误: 启用开机自启失败: %v", err)
					}
				}
			case <-mQuit.ClickedCh:
				log.Println("收到退出请求 (来自托盘菜单)...")
				coreServiceCancel()
				systray.Quit()
				return
			case <-coreServiceCtx.Done():
				log.Println("核心服务上下文已取消，准备退出托盘 (来自coreServiceCtx.Done)。")
				systray.Quit()
				return
			}
		}
	}()
	log.Println("onReady 执行完毕。")
}
func onExit() { log.Println("程序正在退出 (onExit)...") }

func findAndActivateMainWindow() {
	user32 := syscall.NewLazyDLL("user32.dll")
	procFindWindowW := user32.NewProc("FindWindowW")
	procSetForegroundWindow := user32.NewProc("SetForegroundWindow")

	// 尝试查找已有的 Bealink 窗口并激活托盘图标
	className, _ := syscall.UTF16PtrFromString("BealinkPowerEventWnd")
	windowName, _ := syscall.UTF16PtrFromString("Bealink Power Listener")

	hwnd, _, _ := procFindWindowW.Call(
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
	)

	if hwnd != 0 {
		procSetForegroundWindow.Call(hwnd)
	}
}
