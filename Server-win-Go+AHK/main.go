package main

import (
	"context" // 用于 HTTP 服务器优雅关闭
	"fmt"
	"io/ioutil" // 用于读取图标文件
	"log"
	"net" // 用于 IP 地址和网络接口操作
	"net/http"
	"net/url" // *** 用于 URL 解码 ***
	"os"
	"os/exec" // 用于监听退出信号
	"path/filepath"
	"strings" // 用于字符串操作
	"syscall" // 用于调用 Windows API (信号)
	"time"    // 用于超时控制

	"github.com/atotto/clipboard"       // *** 剪贴板库 ***
	"github.com/getlantern/systray"     // 托盘图标库
	"github.com/grandcat/zeroconf"      // Bonjour/mDNS 库
	"golang.org/x/sys/windows/registry" // Windows 注册表操作库
)

// 定义常量
const (
	listenPort         = ":8080"                                         // 服务器监听的端口号 (保持 8080)
	ahkExecutableName  = "AutoHotkey.exe"                                // AHK 可执行文件名 (需放在同目录)
	sleepScriptName    = "sleep_countdown.ahk"                           // 睡眠脚本文件名
	shutdownScriptName = "shutdown_countdown.ahk"                        // 关机脚本文件名
	notifyScriptName   = "notify.ahk"                                    // *** 通知脚本文件名 ***
	iconFileName       = "icon.ico"                                      // 托盘图标文件名 (需放在同目录)
	mDNSServiceName    = "BealinkGo"                                     // Bonjour/mDNS 服务名 (可自定义)
	mDNSServiceType    = "_http._tcp"                                    // HTTP 服务的标准类型
	mDNSDomain         = "local."                                        // mDNS 域
	registryRunPath    = `Software\Microsoft\Windows\CurrentVersion\Run` // 开机自启注册表路径 (当前用户)
	registryValueName  = "BealinkGoServer"                               // 在注册表中为此程序设置的名称
)

// --- 全局变量 ---
var ahkPath string                   // AutoHotkey.exe 的完整路径
var httpServer *http.Server          // HTTP 服务器实例，方便关闭
var consoleHwnd syscall.Handle       // 当前程序的控制台窗口句柄
var consoleVisible bool = true       // 跟踪控制台窗口是否可见 (启动后会设为 false)
var mDNSServer *zeroconf.Server      // mDNS 服务实例，方便关闭
var debugLoggingEnabled bool = false // *** 调试日志开关，默认为 false ***

// --- Windows API 调用准备 ---
var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	user32               = syscall.NewLazyDLL("user32.dll")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procShowWindow       = user32.NewProc("ShowWindow")
)

const (
	SW_HIDE   = 0
	SW_SHOWNA = 8
) // 使用 SW_SHOWNA 尝试修复任务栏图标残留

// --- 控制台窗口操作函数 ---
func getConsoleHwnd() (syscall.Handle, error) {
	ret, _, _ := procGetConsoleWindow.Call()
	hwnd := syscall.Handle(ret)
	if hwnd == 0 {
		return 0, fmt.Errorf("未找到控制台窗口")
	}
	return hwnd, nil
}
func showWindow(hwnd syscall.Handle, command int) bool {
	_, _, err := procShowWindow.Call(uintptr(hwnd), uintptr(command))
	errno := syscall.Errno(0)
	if err != nil && err.Error() != errno.Error() {
		log.Printf("警告: ShowWindow 调用可能失败 (Command: %d): %v\n", command, err)
		return false
	}
	return true
}
func toggleConsoleWindow() {
	if consoleHwnd == 0 {
		log.Println("错误: 控制台句柄无效")
		return
	}
	if consoleVisible {
		if showWindow(consoleHwnd, SW_HIDE) {
			consoleVisible = false
		}
	} else {
		if showWindow(consoleHwnd, SW_SHOWNA) {
			consoleVisible = true
		}
	}
}

// --- 开机自启注册表操作函数 ---
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
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
func enableAutoStart() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	quotedPath := `"` + exePath + `"`
	key, err := registry.OpenKey(registry.CURRENT_USER, registryRunPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err == nil {
		_ = key.DeleteValue(registryValueName)
		key.Close()
	} else if err != registry.ErrNotExist {
		return fmt.Errorf("无法打开注册表键 '%s' 进行清理: %w", registryRunPath, err)
	}
	key, _, err = registry.CreateKey(registry.CURRENT_USER, registryRunPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	err = key.SetStringValue(registryValueName, quotedPath)
	if err != nil {
		return err
	}
	log.Printf("开机自启已启用/更新: %s\n", quotedPath)
	return nil
}
func disableAutoStart() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, registryRunPath, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	defer key.Close()
	err = key.DeleteValue(registryValueName)
	if err != nil && err != registry.ErrNotExist {
		return err
	}
	log.Println("开机自启已禁用。")
	return nil
}

// --- findLocalAhkPath 函数 ---
func findLocalAhkPath() (string, error) {
	exePath, err := os.Executable()
	var dir string
	if err != nil {
		dir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("无法获取程序路径和工作目录")
		}
	} else {
		dir = filepath.Dir(exePath)
	}
	localAhkPath := filepath.Join(dir, ahkExecutableName)
	if _, err := os.Stat(localAhkPath); err == nil {
		return localAhkPath, nil
	}
	return "", fmt.Errorf("未在 %s 找到 %s", dir, ahkExecutableName)
}

// --- runAhkScript 函数 ---
func runAhkScript(scriptName string, args ...string) error {
	if ahkPath == "" {
		var findErr error
		ahkPath, findErr = findLocalAhkPath()
		if findErr != nil {
			return fmt.Errorf("AHK 路径未设置且无法找到")
		}
	}
	exePath, err := os.Executable()
	var scriptDir string
	if err != nil {
		scriptDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("无法获取脚本目录")
		}
	} else {
		scriptDir = filepath.Dir(exePath)
	}
	scriptFullPath := filepath.Join(scriptDir, scriptName)
	if _, err := os.Stat(scriptFullPath); os.IsNotExist(err) {
		return fmt.Errorf("脚本 %s 未在 %s 找到", scriptName, scriptDir)
	}
	cmdArgs := []string{scriptFullPath}
	cmdArgs = append(cmdArgs, args...)
	// 仅在调试模式下打印完整参数，避免日志过长
	if debugLoggingEnabled {
		log.Printf("执行: %s %v\n", ahkPath, cmdArgs)
	} else {
		log.Printf("执行 AHK 脚本: %s (带 %d 个参数)\n", scriptName, len(args))
	}
	cmd := exec.Command(ahkPath, cmdArgs...)
	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("启动脚本 %s 失败: %w", scriptFullPath, err)
	}
	// 简化日志
	log.Printf("脚本 %s 已启动 (PID: %d)\n", scriptName, cmd.Process.Pid)
	go cmd.Wait()
	return nil
}

// --- HTTP 处理函数 ---
func handleRoot(w http.ResponseWriter, r *http.Request) {
	if debugLoggingEnabled && r.URL.Path != "/favicon.ico" {
		log.Printf("请求: %s %s from %s\n", r.Method, r.URL.Path, r.RemoteAddr)
	}
	if r.URL.Path == "/favicon.ico" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "Bealink Go 服务运行中。\n访问 /sleep, /shutdown, /clip/<text>。\n")
}
func handlePing(w http.ResponseWriter, r *http.Request) {
	if debugLoggingEnabled && r.URL.Path != "/favicon.ico" {
		log.Printf("请求: %s %s from %s\n", r.Method, r.URL.Path, r.RemoteAddr)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "pong")
}
func handleSleep(w http.ResponseWriter, r *http.Request) {
	if debugLoggingEnabled {
		log.Printf("收到 /sleep 请求 from %s\n", r.RemoteAddr)
	}
	err := runAhkScript(sleepScriptName)
	if err != nil {
		log.Printf("错误: 调用睡眠脚本失败: %v\n", err)
		http.Error(w, "启动睡眠脚本失败。", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "睡眠倒计时已启动。\n")
}
func handleShutdown(w http.ResponseWriter, r *http.Request) {
	if debugLoggingEnabled {
		log.Printf("收到 /shutdown 请求 from %s\n", r.RemoteAddr)
	}
	err := runAhkScript(shutdownScriptName)
	if err != nil {
		log.Printf("错误: 调用关机脚本失败: %v\n", err)
		http.Error(w, "启动关机脚本失败。", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "关机倒计时已启动。\n")
}
func handleClip(w http.ResponseWriter, r *http.Request) {
	if debugLoggingEnabled {
		log.Printf("收到剪贴板请求: %s %s from %s\n", r.Method, r.URL.Path, r.RemoteAddr)
	}
	encodedText := strings.TrimPrefix(r.URL.Path, "/clip/")
	if encodedText == "" {
		http.Error(w, "格式：/clip/<文本内容>", http.StatusBadRequest)
		return
	}
	textToCopy, err := url.PathUnescape(encodedText)
	if err != nil {
		log.Printf("错误: URL 解码失败: %v\n", err)
		http.Error(w, "无法解码文本。", http.StatusBadRequest)
		return
	}
	err = clipboard.WriteAll(textToCopy)
	if err != nil {
		log.Printf("错误: 写入剪贴板失败: %v\n", err)
		http.Error(w, "无法写入剪贴板。", http.StatusInternalServerError)
		return
	}
	log.Printf("成功写入剪贴板: %s\n", textToCopy) // 保留这个成功日志

	// *** 移除长度限制，直接传递 textToCopy ***
	notificationText := textToCopy
	err = runAhkScript(notifyScriptName, notificationText) // 调用 notify.ahk 并传递完整文本
	if err != nil {
		log.Printf("警告: 调用通知脚本失败: %v\n", err)
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	respConfirm := textToCopy
	if len(respConfirm) > 50 {
		respConfirm = respConfirm[:50] + "..."
	}
	fmt.Fprintf(w, "文本已复制到剪贴板: %s\n", respConfirm)
}

// --- getLocalIP 函数 ---
func getLocalIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	var candidateIPs []string
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagPointToPoint != 0 || strings.Contains(strings.ToLower(iface.Name), "virtual") || strings.Contains(strings.ToLower(iface.Name), "vmware") || strings.Contains(strings.ToLower(iface.Name), "vbox") || strings.Contains(strings.ToLower(iface.Name), "docker") || strings.Contains(strings.ToLower(iface.Name), "wsl") || strings.Contains(strings.ToLower(iface.Name), "loopback") || strings.Contains(strings.ToLower(iface.Name), "tap") || strings.Contains(strings.ToLower(iface.Name), "tun") || strings.Contains(strings.ToLower(iface.Name), "ppp") || strings.Contains(strings.ToLower(iface.Name), "wg") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				continue
			}
			ipStr := ip.String()
			if strings.HasPrefix(ipStr, "169.254.") || strings.HasPrefix(ipStr, "198.18.") || strings.HasPrefix(ipStr, "172.17.") || strings.HasPrefix(ipStr, "172.18.") || strings.HasPrefix(ipStr, "172.19.") || strings.HasPrefix(ipStr, "172.20.") || strings.HasPrefix(ipStr, "172.21.") || strings.HasPrefix(ipStr, "172.22.") || strings.HasPrefix(ipStr, "172.23.") || strings.HasPrefix(ipStr, "172.24.") || strings.HasPrefix(ipStr, "172.25.") || strings.HasPrefix(ipStr, "172.26.") || strings.HasPrefix(ipStr, "172.27.") || strings.HasPrefix(ipStr, "172.28.") || strings.HasPrefix(ipStr, "172.29.") || strings.HasPrefix(ipStr, "172.30.") || strings.HasPrefix(ipStr, "172.31.") {
				continue
			}
			candidateIPs = append(candidateIPs, ipStr)
		}
	}
	if len(candidateIPs) > 0 {
		log.Printf("最终选择 IP: %s\n", candidateIPs[0])
		return candidateIPs[0]
	}
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		log.Printf("通过 Dial 方法找到 IP: %s\n", localAddr.IP.String())
		return localAddr.IP.String()
	}
	log.Println("警告: 未找到合适的 IP, 回退到 127.0.0.1")
	return "127.0.0.1"
}

// --- loadIcon 函数 ---
func loadIcon(fileName string) ([]byte, error) {
	exePath, err := os.Executable()
	var dir string
	if err != nil {
		dir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	} else {
		dir = filepath.Dir(exePath)
	}
	iconPath := filepath.Join(dir, fileName)
	content, err := ioutil.ReadFile(iconPath)
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("图标 %s 为空", iconPath)
	}
	return content, nil
}

// --- main 函数 ---
func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("程序启动，初始化系统托盘...")
	var err error
	consoleHwnd, err = getConsoleHwnd()
	if err != nil {
		log.Printf("启动警告: %v\n", err)
	}
	systray.Run(onReady, onExit)
	log.Println("程序正常退出。")
}

// --- onReady 函数 ---
func onReady() {
	log.Println("系统托盘 onReady 开始执行。")
	if consoleHwnd != 0 {
		if showWindow(consoleHwnd, SW_HIDE) {
			consoleVisible = false
		}
	}
	iconBytes, err := loadIcon(iconFileName)
	if err != nil {
		log.Printf("错误: 加载图标失败: %v\n", err)
	} else {
		systray.SetIcon(iconBytes)
	}
	systray.SetTitle("Bealink Go 服务")
	systray.SetTooltip("Bealink Go (" + listenPort + ")")
	mConsole := systray.AddMenuItem("显示控制台", "显示/隐藏窗口")
	mDebugLog := systray.AddMenuItem("启用调试日志", "切换详细日志记录")
	mAutoStart := systray.AddMenuItem("开机自启", "切换开机自启")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "关闭程序")
	if debugLoggingEnabled {
		mDebugLog.Check()
	} else {
		mDebugLog.Uncheck()
	}
	go func() {
		enabled, err := isAutoStartEnabled()
		if err == nil {
			if enabled {
				mAutoStart.Check()
			} else {
				mAutoStart.Uncheck()
			}
		} else {
			mAutoStart.Disable()
			mAutoStart.SetTitle("开机自启(错误)")
		}
	}()
	coreServiceCtx, coreServiceCancel := context.WithCancel(context.Background())
	go startCoreServices(coreServiceCtx)
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
			case <-mDebugLog.ClickedCh:
				debugLoggingEnabled = !debugLoggingEnabled
				if debugLoggingEnabled {
					mDebugLog.Check()
					log.Println("调试日志已启用。")
				} else {
					mDebugLog.Uncheck()
					log.Println("调试日志已禁用。")
				}
			case <-mAutoStart.ClickedCh:
				if mAutoStart.Checked() {
					if err := disableAutoStart(); err == nil {
						mAutoStart.Uncheck()
					} else {
						log.Printf("禁用自启失败: %v", err)
					}
				} else {
					if err := enableAutoStart(); err == nil {
						mAutoStart.Check()
					} else {
						log.Printf("启用自启失败: %v", err)
					}
				}
			case <-mQuit.ClickedCh:
				log.Println("收到退出菜单请求...")
				coreServiceCancel()
				systray.Quit()
				return
			}
		}
	}()
	log.Println("onReady 执行完毕。")
}

// --- 核心服务启动函数 ---
func startCoreServices(ctx context.Context) {
	log.Println("核心服务 goroutine 启动...")
	var findErr error
	ahkPath, findErr = findLocalAhkPath()
	if findErr != nil {
		log.Printf("启动警告: %v", findErr)
	}
	portStr := strings.TrimPrefix(listenPort, ":")
	port, err := net.LookupPort("tcp", portStr)
	if err != nil {
		log.Printf("致命错误: 无效端口 %s: %v\n", portStr, err)
		return
	}
	hostname, err := os.Hostname()
	if err != nil {
		hostname = mDNSServiceName
	} else {
		hostname = strings.TrimSuffix(hostname, ".local")
	}
	instanceName := fmt.Sprintf("%s", hostname)
	log.Printf("注册 mDNS: %s:%d\n", instanceName, port)
	var mDNSErr error
	mDNSServer, mDNSErr = zeroconf.Register(instanceName, mDNSServiceType, mDNSDomain, port, []string{"txtv=0"}, nil)
	if mDNSErr != nil {
		log.Printf("警告: mDNS 注册失败: %v\n", mDNSErr)
	} else {
		log.Println("mDNS 注册成功！")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("/ping", handlePing)
	mux.HandleFunc("/sleep", handleSleep)
	mux.HandleFunc("/shutdown", handleShutdown)
	mux.HandleFunc("/clip/", handleClip) // 添加 clip 路由
	httpServer = &http.Server{Addr: listenPort, Handler: mux}
	httpServerErrChan := make(chan error, 1)
	go func() {
		log.Printf("HTTP 服务器准备监听于 %s...\n", httpServer.Addr)
		localIP := getLocalIP()
		log.Printf("访问: http://%s%s 或 http://%s.%s:%d\n", localIP, listenPort, instanceName, mDNSDomain, port)
		httpServerErrChan <- httpServer.ListenAndServe()
		close(httpServerErrChan)
		log.Println("HTTP 服务器监听 goroutine 结束。")
	}()
	select {
	case err := <-httpServerErrChan:
		if err != nil && err != http.ErrServerClosed {
			log.Printf("!!! HTTP 服务器启动或运行时发生错误: %v !!!\n", err)
			systray.SetTooltip("错误: HTTP 服务未能启动！")
		} else if err == http.ErrServerClosed {
			log.Println("HTTP 服务器已被关闭。")
		}
	case <-ctx.Done():
		log.Println("核心服务收到退出信号 (ctx.Done)，开始清理...")
		log.Println("正在关闭 HTTP 服务器...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP 服务器关闭错误: %v\n", err)
		} else {
			log.Println("HTTP 服务器已关闭。")
		}
	}
	if mDNSServer != nil {
		log.Println("正在注销 mDNS 服务...")
		mDNSServer.Shutdown()
		log.Println("mDNS 服务已注销。")
	}
	log.Println("核心服务 goroutine 清理完毕，即将退出。")
}

// --- onExit 函数 ---
func onExit() {
	log.Println("系统托盘退出 (onExit)，程序即将终止。")
	if consoleHwnd != 0 && !consoleVisible {
		showWindow(consoleHwnd, SW_SHOWNA)
	}
}
