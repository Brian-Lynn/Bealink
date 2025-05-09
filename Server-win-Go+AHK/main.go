package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/atotto/clipboard"
	"github.com/getlantern/systray"
	"github.com/grandcat/zeroconf"
	"golang.org/x/sys/windows/registry"
)

// 定义常量
const (
	listenPort         = ":8080"
	ahkExecutableName  = "AutoHotkey.exe" // AHK主程序名
	sleepScriptName    = "sleep_countdown.ahk"
	shutdownScriptName = "shutdown_countdown.ahk"
	notifyScriptName   = "notify.ahk"
	iconFileName       = "icon.ico"
	mDNSServiceName    = "BealinkGo"  // mDNS 服务名
	mDNSServiceType    = "_http._tcp" // mDNS 服务类型
	mDNSDomain         = "local."     // mDNS 域名
	registryRunPath    = `Software\Microsoft\Windows\CurrentVersion\Run`
	registryValueName  = "BealinkGoServer" // 注册表自启动项名称
)

// --- 全局变量 ---
var (
	ahkPath             string                       // AutoHotkey.exe 的路径
	httpServer          *http.Server                 // HTTP 服务器实例
	consoleHwnd         syscall.Handle               // 控制台窗口句柄
	consoleVisible      bool             = true      // 控制台窗口是否可见
	mDNSServer          *zeroconf.Server             // mDNS 服务实例
	debugLoggingEnabled bool             = false     // 是否启用详细日志记录
	originalLogOutput   io.Writer        = os.Stderr // 原始日志输出目标
)

// --- Windows API 调用准备 ---
var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	user32               = syscall.NewLazyDLL("user32.dll")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow") // 获取控制台窗口句柄
	procShowWindow       = user32.NewProc("ShowWindow")         // 显示/隐藏窗口
	procSendMessage      = user32.NewProc("SendMessageW")       // 发送消息
)

// Windows API 常量
const (
	SW_HIDE   = 0 // 隐藏窗口
	SW_SHOWNA = 8 // 显示窗口但不激活

	// 用于关闭显示器的 Windows 消息常量
	WM_SYSCOMMAND   = 0x0112          // 系统命令消息
	SC_MONITORPOWER = 0xF170          // 显示器电源相关的系统命令
	MONITOR_OFF     = 2               // 参数：关闭显示器 (0: On, 1: LowPower, 2: Off)
	HWND_BROADCAST  = uintptr(0xffff) // 广播消息给所有顶级窗口

	ERROR_ACCESS_DENIED syscall.Errno = 5 // 定义 Access Denied 错误码
)

// --- 控制台窗口操作 ---
func getConsoleHwnd() syscall.Handle {
	ret, _, _ := procGetConsoleWindow.Call()
	return syscall.Handle(ret)
}

func showWindow(hwnd syscall.Handle, command int) {
	if hwnd == 0 {
		return
	}
	procShowWindow.Call(uintptr(hwnd), uintptr(command))
}

func toggleConsoleWindow() {
	if consoleHwnd == 0 {
		consoleHwnd = getConsoleHwnd()
		if consoleHwnd == 0 {
			log.Println("错误: 未能获取控制台窗口句柄。")
			return
		}
	}
	if consoleVisible {
		showWindow(consoleHwnd, SW_HIDE)
		consoleVisible = false
	} else {
		showWindow(consoleHwnd, SW_SHOWNA)
		consoleVisible = true
	}
}

// --- 开机自启相关 ---
func isAutoStartEnabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, registryRunPath, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, err
	}
	defer key.Close()
	_, _, err = key.GetStringValue(registryValueName)
	return err == nil, nil
}

func enableAutoStart() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取程序路径失败: %w", err)
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, registryRunPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("创建注册表键失败: %w", err)
	}
	defer key.Close()
	quotedPath := `"` + exePath + `"` // 路径加引号以处理空格
	err = key.SetStringValue(registryValueName, quotedPath)
	if err != nil {
		return fmt.Errorf("写入注册表值失败: %w", err)
	}
	log.Println("开机自启已启用。")
	return nil
}

func disableAutoStart() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, registryRunPath, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			log.Println("开机自启项不存在，无需禁用。")
			return nil
		}
		return fmt.Errorf("打开注册表键失败: %w", err)
	}
	defer key.Close()
	err = key.DeleteValue(registryValueName)
	if err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("删除注册表值失败: %w", err)
	}
	log.Println("开机自启已禁用。")
	return nil
}

// --- AHK 脚本执行 ---
func findAhkPath() (string, error) {
	// 优先从程序同目录查找
	exePath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exePath)
		localAhkPath := filepath.Join(dir, ahkExecutableName)
		if _, statErr := os.Stat(localAhkPath); statErr == nil {
			return localAhkPath, nil
		}
	}
	// 如果找不到，尝试从环境变量 PATH 中查找
	path, err := exec.LookPath(ahkExecutableName)
	if err == nil {
		return path, nil
	}
	return "", fmt.Errorf("未在程序目录或系统PATH中找到 %s", ahkExecutableName)
}

func runAhkScript(scriptName string, args ...string) error {
	if ahkPath == "" {
		var findErr error
		ahkPath, findErr = findAhkPath()
		if findErr != nil {
			return findErr // 直接返回错误，不记录日志，让调用者处理
		}
	}
	scriptDir := filepath.Dir(os.Args[0]) // 脚本通常和主程序在同一目录
	scriptFullPath := filepath.Join(scriptDir, scriptName)

	if _, err := os.Stat(scriptFullPath); os.IsNotExist(err) {
		return fmt.Errorf("脚本 %s 未找到", scriptFullPath)
	}

	cmdArgs := append([]string{scriptFullPath}, args...)
	cmd := exec.Command(ahkPath, cmdArgs...)

	if debugLoggingEnabled {
		log.Printf("执行 AHK: %s %v", ahkPath, cmdArgs)
	}

	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("启动脚本 %s 失败: %w", scriptName, err)
	}
	log.Printf("脚本 %s 已启动。", scriptName) // 简洁日志

	go func() { // 异步等待脚本结束，避免阻塞
		cmd.Wait() // waitErr 可以忽略，因为是轻量程序
	}()
	return nil
}

// --- HTTP 处理函数 ---
func handleRoot(w http.ResponseWriter, r *http.Request) {
	if debugLoggingEnabled && r.URL.Path != "/favicon.ico" {
		log.Printf("请求: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
	}
	if r.URL.Path == "/favicon.ico" {
		http.NotFound(w, r)
		return
	}
	fmt.Fprintln(w, "Bealink Go 服务运行中。可用端点: /sleep, /shutdown, /clip/<text>, /monitor-off, /ping")
}

func handlePing(w http.ResponseWriter, r *http.Request) {
	if debugLoggingEnabled {
		log.Printf("请求: /ping from %s", r.RemoteAddr)
	}
	fmt.Fprintln(w, "pong")
}

func handleSleep(w http.ResponseWriter, r *http.Request) {
	if debugLoggingEnabled {
		log.Printf("请求: /sleep from %s", r.RemoteAddr)
	}
	err := runAhkScript(sleepScriptName)
	if err != nil {
		log.Printf("错误: 调用睡眠脚本失败: %v", err) // 适当记录错误
		http.Error(w, "启动睡眠脚本失败。", http.StatusInternalServerError)
		return
	}
	fmt.Fprintln(w, "睡眠倒计时已启动。")
}

func handleShutdown(w http.ResponseWriter, r *http.Request) {
	if debugLoggingEnabled {
		log.Printf("请求: /shutdown from %s", r.RemoteAddr)
	}
	err := runAhkScript(shutdownScriptName)
	if err != nil {
		log.Printf("错误: 调用关机脚本失败: %v", err)
		http.Error(w, "启动关机脚本失败。", http.StatusInternalServerError)
		return
	}
	fmt.Fprintln(w, "关机倒计时已启动。")
}

func handleClip(w http.ResponseWriter, r *http.Request) {
	if debugLoggingEnabled {
		log.Printf("请求: %s from %s", r.URL.Path, r.RemoteAddr)
	}
	encodedText := strings.TrimPrefix(r.URL.Path, "/clip/")
	if encodedText == "" {
		http.Error(w, "格式错误，请使用 /clip/<要复制的文本>", http.StatusBadRequest)
		return
	}
	textToCopy, err := url.PathUnescape(encodedText)
	if err != nil {
		log.Printf("错误: URL解码失败: %v", err)
		http.Error(w, "无法解码文本。", http.StatusBadRequest)
		return
	}
	if err := clipboard.WriteAll(textToCopy); err != nil {
		log.Printf("错误: 写入剪贴板失败: %v", err)
		http.Error(w, "无法写入剪贴板。", http.StatusInternalServerError)
		return
	}
	log.Printf("文本已复制到剪贴板: %s", textToCopy) // 关键操作，保留日志
	if err := runAhkScript(notifyScriptName, textToCopy); err != nil {
		log.Printf("警告: 调用通知脚本失败: %v", err) // 通知失败是次要的
	}
	fmt.Fprintf(w, "文本已复制到剪贴板: %s\n", textToCopy)
}

// 修改后的：关闭显示器的函数
func turnOffMonitor() error {
	_, _, err := procSendMessage.Call(HWND_BROADCAST, WM_SYSCOMMAND, SC_MONITORPOWER, MONITOR_OFF)

	// err 是 syscall.Errno 类型。
	// syscall.Errno(0) (ERROR_SUCCESS) 表示成功。
	// 非零 syscall.Errno 值表示错误。
	if err != syscall.Errno(0) { // 修正：与 syscall.Errno(0) 比较
		// 检查错误是否是 ERROR_ACCESS_DENIED (值为 5)
		// 我们直接使用预定义的 ERROR_ACCESS_DENIED 常量进行比较
		if err == ERROR_ACCESS_DENIED {
			// API 返回了 "Access is denied"，但根据经验，显示器通常仍会关闭。
			// 我们将此记录为警告，并认为操作对于关闭显示器这个主要目的是成功的。
			log.Println("警告: SendMessage (SC_MONITORPOWER) API 调用返回 'Access is denied'。显示器通常仍会关闭。")
			// 不返回错误，因为我们期望主要功能（关闭显示器）仍然有效。
		} else {
			// 对于任何其他错误，它是未预期的，因此将其报告为失败。
			return fmt.Errorf("SendMessage (SC_MONITORPOWER) API 调用失败: %w", err)
		}
	}
	// 如果 err 是 syscall.Errno(0) (SUCCESS) 或 ERROR_ACCESS_DENIED (已在上面处理), 则记录命令已发送。
	log.Println("关闭显示器命令已发送。")
	return nil
}

// 新增：处理 /monitor-off 请求的函数
func handleMonitorOff(w http.ResponseWriter, r *http.Request) {
	if debugLoggingEnabled {
		log.Printf("请求: /monitor-off from %s", r.RemoteAddr)
	}
	err := turnOffMonitor() // 调用修改后的函数
	if err != nil {         // 如果 turnOffMonitor 返回了其他类型的错误
		log.Printf("错误: 关闭显示器操作失败: %v", err)
		http.Error(w, "关闭显示器失败。", http.StatusInternalServerError)
		return
	}
	fmt.Fprintln(w, "关闭显示器的命令已发送。")
}

// --- 工具函数 ---
func getLocalIP() string { // 简化版IP获取，选择第一个合适的非环回IPv4
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

func loadIconBytes(fileName string) []byte {
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("警告: 获取程序路径失败，可能无法加载图标: %v", err)
		return nil
	}
	iconPath := filepath.Join(filepath.Dir(exePath), fileName)
	iconBytes, err := ioutil.ReadFile(iconPath)
	if err != nil {
		log.Printf("警告: 加载图标 %s 失败: %v", iconPath, err)
		return nil
	}
	return iconBytes
}

func setDebugLogging(enabled bool) {
	debugLoggingEnabled = enabled
	if debugLoggingEnabled {
		log.SetOutput(originalLogOutput)
		log.Println("调试日志已启用。")
	} else {
		log.Println("调试日志已禁用。") // 确保这条能输出
		log.SetOutput(ioutil.Discard)
	}
}

// --- 主程序与系统托盘 ---
func main() {
	originalLogOutput = log.Writer() // 保存log包默认的输出
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("程序启动...")

	consoleHwnd = getConsoleHwnd() // 尽早获取句柄

	// 默认禁用调试日志，除非显式开启
	if !debugLoggingEnabled {
		// 在程序启动时，如果调试日志是默认关闭的，先用原始输出打印一条提示信息
		fmt.Fprintln(originalLogOutput, time.Now().Format("2006/01/02 15:04:05 main.go:")+" 默认禁用调试日志。可通过托盘菜单启用。")
		log.SetOutput(ioutil.Discard)
	}

	systray.Run(onReady, onExit)
}

func onReady() {
	// 确保 onReady 期间的日志能输出
	currentLogOutput := log.Writer()
	log.SetOutput(originalLogOutput)

	log.Println("系统托盘准备就绪。")
	if consoleHwnd != 0 { // 默认隐藏控制台
		showWindow(consoleHwnd, SW_HIDE)
		consoleVisible = false
	}

	systray.SetIcon(loadIconBytes(iconFileName))
	systray.SetTitle("Bealink Go 服务")
	systray.SetTooltip("Bealink Go (" + listenPort + ")")

	mConsole := systray.AddMenuItem("显示控制台", "显示/隐藏控制台窗口")
	if !consoleVisible { // 根据初始状态设置
		mConsole.SetTitle("显示控制台")
	} else {
		mConsole.SetTitle("隐藏控制台")
	}

	mDebug := systray.AddMenuItem("启用调试日志", "切换详细日志输出")
	if debugLoggingEnabled {
		mDebug.Check()
	}

	mAutoStart := systray.AddMenuItem("开机自启", "设置/取消开机自启")
	autoStartEnabled, err := isAutoStartEnabled()
	if err == nil && autoStartEnabled {
		mAutoStart.Check()
	} else if err != nil {
		log.Printf("错误: 检查开机自启状态失败: %v", err)
		mAutoStart.Disable()
	}

	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "关闭服务")

	// 启动核心服务 (HTTP, mDNS)
	coreServiceCtx, coreServiceCancel := context.WithCancel(context.Background())
	go startCoreServices(coreServiceCtx)

	// 托盘菜单事件处理
	go func() {
		for {
			select {
			case <-mConsole.ClickedCh:
				toggleConsoleWindow()
				if consoleVisible {
					mConsole.SetTitle("隐藏控制台")
				} else {
					mConsole.SetTitle("显示控制台")
				}
			case <-mDebug.ClickedCh:
				setDebugLogging(!debugLoggingEnabled)
				if debugLoggingEnabled {
					mDebug.Check()
				} else {
					mDebug.Uncheck()
				}
			case <-mAutoStart.ClickedCh:
				if mAutoStart.Checked() {
					if err := disableAutoStart(); err == nil {
						mAutoStart.Uncheck()
					} else {
						log.Printf("错误: 禁用开机自启失败: %v", err)
					}
				} else {
					if err := enableAutoStart(); err == nil {
						mAutoStart.Check()
					} else {
						log.Printf("错误: 启用开机自启失败: %v", err)
					}
				}
			case <-mQuit.ClickedCh:
				log.Println("收到退出请求...")
				coreServiceCancel() // 通知核心服务停止
				systray.Quit()      // 退出托盘
				return
			}
		}
	}()
	log.Println("onReady 执行完毕。")
	// 恢复 onReady 执行前的日志输出状态
	if !debugLoggingEnabled && currentLogOutput == ioutil.Discard {
		log.SetOutput(ioutil.Discard)
	} else if currentLogOutput != originalLogOutput { // 如果之前不是原始输出，恢复它
		log.SetOutput(currentLogOutput)
	}
}

func onExit() {
	log.SetOutput(originalLogOutput) // 确保退出日志能输出
	log.Println("程序正在退出...")
	if consoleHwnd != 0 && !consoleVisible { // 退出时显示控制台，方便查看最后日志
		showWindow(consoleHwnd, SW_SHOWNA)
	}
}

func startCoreServices(ctx context.Context) {
	// 确保核心服务启动时的日志能输出
	currentGoroutineLogOutput := log.Writer()
	log.SetOutput(originalLogOutput)
	log.Println("核心服务启动中...")

	// 初始化 AHK 路径
	var findErr error
	ahkPath, findErr = findAhkPath()
	if findErr != nil {
		log.Printf("警告: %v (AHK 相关功能可能不可用)", findErr)
	}

	// mDNS 服务注册
	portInt, _ := net.LookupPort("tcp", strings.TrimPrefix(listenPort, ":"))
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "BealinkGoHost"
	}
	var mDNSErr error
	mDNSServer, mDNSErr = zeroconf.Register(hostname, mDNSServiceType, mDNSDomain, portInt, []string{"path=/"}, nil)
	if mDNSErr != nil {
		log.Printf("警告: mDNS 服务注册失败: %v", mDNSErr)
	} else {
		log.Println("mDNS 服务注册成功。")
	}

	// HTTP 服务器设置
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("/ping", handlePing)
	mux.HandleFunc("/sleep", handleSleep)
	mux.HandleFunc("/shutdown", handleShutdown)
	mux.HandleFunc("/clip/", handleClip)
	mux.HandleFunc("/monitor-off", handleMonitorOff) // 注册新端点

	httpServer = &http.Server{Addr: listenPort, Handler: mux}
	log.Printf("HTTP 服务监听于: %s (本机IP: %s)", listenPort, getLocalIP())
	if mDNSServer != nil {
		log.Printf("mDNS 可访问地址: http://%s.%s:%d", hostname, mDNSDomain, portInt)
	}

	// 根据调试状态决定后续日志输出
	if !debugLoggingEnabled {
		log.SetOutput(ioutil.Discard)
	} else {
		log.SetOutput(originalLogOutput)
	}

	// 启动 HTTP 服务器的 Goroutine
	httpServerErrChan := make(chan error, 1)
	go func() {
		// ListenAndServe 的内部日志我们无法直接控制，但可以控制我们自己的log
		// 在这个goroutine内部，如果debugLoggingEnabled为false，log的输出已经是ioutil.Discard
		err := httpServer.ListenAndServe()
		// 服务结束后，恢复日志输出，确保能看到结束信息
		log.SetOutput(originalLogOutput)
		if err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP 服务器监听 goroutine 意外结束: %v", err)
		} else {
			log.Println("HTTP 服务器监听 goroutine 正常结束。")
		}
		httpServerErrChan <- err // 将错误或 nil 发送回主 goroutine
		close(httpServerErrChan)
	}()

	// 等待上下文取消 (程序退出信号) 或 HTTP 服务器出错
	select {
	case err := <-httpServerErrChan:
		log.SetOutput(originalLogOutput) // 确保错误日志能输出
		if err != nil && err != http.ErrServerClosed {
			log.Printf("!!! HTTP 服务器错误: %v", err)
			systray.SetTooltip("错误: HTTP服务故障")
		}
	case <-ctx.Done():
		log.SetOutput(originalLogOutput) // 确保退出日志能输出
		log.Println("收到退出信号，开始关闭核心服务...")
		// 关闭 HTTP 服务器
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP 服务器关闭错误: %v", err)
		} else {
			log.Println("HTTP 服务器已关闭。")
		}
	}

	// 清理 mDNS
	if mDNSServer != nil {
		mDNSServer.Shutdown()
		log.Println("mDNS 服务已注销。")
	}
	log.Println("核心服务已停止。")
	// 恢复此 goroutine 开始时的日志设置
	if !debugLoggingEnabled && currentGoroutineLogOutput == ioutil.Discard {
		log.SetOutput(ioutil.Discard)
	} else if currentGoroutineLogOutput != originalLogOutput {
		log.SetOutput(currentGoroutineLogOutput)
	}
}
