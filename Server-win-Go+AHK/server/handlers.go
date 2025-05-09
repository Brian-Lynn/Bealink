package server

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"bealinkserver/ahk" // 确保替换为您的模块名
	"bealinkserver/logging"
	"bealinkserver/winapi"

	"github.com/atotto/clipboard"
	"github.com/gorilla/websocket"
)

//go:embed templates
var templateFS embed.FS

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// 允许所有来源的WebSocket连接，生产环境中应更严格
		return true
	},
}

var (
	activeSleepProcess    *os.Process
	activeShutdownProcess *os.Process
	sleepMutex            sync.Mutex
	shutdownMutex         sync.Mutex
)

// clearProcess 异步等待一个进程完成并清除其引用。
func clearProcess(p **os.Process, m *sync.Mutex, taskName string) {
	if p == nil || *p == nil {
		return
	}
	processToWait := *p // 复制进程指针

	go func() {
		if processToWait == nil {
			return
		}
		pid := processToWait.Pid
		log.Printf("开始等待 %s AHK 脚本 (PID: %d) 结束...", taskName, pid)
		state, err := processToWait.Wait() // 等待进程退出
		if err != nil {
			log.Printf("等待 %s AHK 脚本 (PID: %d) 结束时发生错误: %v", taskName, pid, err)
		} else {
			log.Printf("%s AHK 脚本 (PID: %d) 已结束，退出状态: %s", taskName, pid, state.String())
		}

		m.Lock()
		// 只有当全局变量仍然指向我们等待的这个进程时，才将其清空
		if *p != nil && (*p).Pid == pid {
			*p = nil
			log.Printf("已清除活动的 %s 进程引用 (PID: %d)。", taskName, pid)
		} else {
			log.Printf("等待 %s (PID: %d) 结束，但全局引用已指向其他进程或为nil。", taskName, pid)
		}
		m.Unlock()
	}()
}

// handleRoot 处理根路径请求，显示服务信息。
func handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/favicon.ico" {
		http.NotFound(w, r)
		return
	}
	hostname, _ := os.Hostname() // 获取本机主机名
	localIP := getLocalIP()      // 获取本机IP (应在 server.go 中定义)

	// 使用在 server.go 中设置的全局变量 GlobalActualListenAddr 和 GlobalActualPort
	fmt.Fprintf(w, "Bealink Go 服务运行中。\n监听于: %s (或 http://localhost:%s)\n通过IP访问: http://%s:%s\n通过主机名(mDNS): http://%s.local:%s\n可用端点: /sleep, /shutdown, /clip/<text>, /monitor, /ping, /debug",
		GlobalActualListenAddr, GlobalActualPort, // 这些变量在 server.go 中定义和设置
		localIP, GlobalActualPort,
		hostname, GlobalActualPort)
}

// handlePing 处理 /ping 请求，返回 "pong"。
func handlePing(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "pong 🏓") // 加个小 emoji
}

// handleSleep 处理 /sleep 请求，启动或取消睡眠倒计时。
func handleSleep(w http.ResponseWriter, r *http.Request) {
	sleepMutex.Lock()
	defer sleepMutex.Unlock()

	if activeSleepProcess != nil {
		log.Printf("检测到活动的睡眠进程 (PID: %d)，尝试取消...", activeSleepProcess.Pid)
		err := activeSleepProcess.Kill() // 尝试终止已存在的进程
		if err != nil {
			log.Printf("错误: 取消睡眠任务 (PID: %d) 失败: %v", activeSleepProcess.Pid, err)
			http.Error(w, "取消睡眠任务失败 ❌", http.StatusInternalServerError)
			return
		}
		log.Printf("睡眠任务 (PID: %d) 已成功发送终止信号。", activeSleepProcess.Pid)
		fmt.Fprintln(w, "睡眠任务已取消 💤") // 极简响应
	} else {
		log.Println("没有活动的睡眠任务，准备启动新任务...")
		process, err := ahk.RunScriptAndGetProcess("sleep_countdown.ahk") // 运行AHK脚本
		if err != nil {
			log.Printf("错误: 启动睡眠脚本失败: %v", err)
			http.Error(w, "启动睡眠脚本失败 ❌", http.StatusInternalServerError)
			return
		}
		activeSleepProcess = process                            // 保存进程引用
		go clearProcess(&activeSleepProcess, &sleepMutex, "睡眠") // 异步等待并清理
		fmt.Fprintln(w, "睡眠倒计时已启动 😴")                           // 极简响应
	}
}

// handleShutdown 处理 /shutdown 请求，启动或取消关机倒计时。
func handleShutdown(w http.ResponseWriter, r *http.Request) {
	shutdownMutex.Lock()
	defer shutdownMutex.Unlock()

	if activeShutdownProcess != nil {
		log.Printf("检测到活动的关机进程 (PID: %d)，尝试取消...", activeShutdownProcess.Pid)
		err := activeShutdownProcess.Kill()
		if err != nil {
			log.Printf("错误: 取消关机任务 (PID: %d) 失败: %v", activeShutdownProcess.Pid, err)
			http.Error(w, "取消关机任务失败 ❌", http.StatusInternalServerError)
			return
		}
		log.Printf("关机任务 (PID: %d) 已成功发送终止信号。", activeShutdownProcess.Pid)
		fmt.Fprintln(w, "关机任务已取消 🚫") // 极简响应
	} else {
		log.Println("没有活动的关机任务，准备启动新任务...")
		process, err := ahk.RunScriptAndGetProcess("shutdown_countdown.ahk")
		if err != nil {
			log.Printf("错误: 启动关机脚本失败: %v", err)
			http.Error(w, "启动关机脚本失败 ❌", http.StatusInternalServerError)
			return
		}
		activeShutdownProcess = process
		go clearProcess(&activeShutdownProcess, &shutdownMutex, "关机")
		fmt.Fprintln(w, "关机倒计时已启动 ⏳") // 极简响应
	}
}

// handleClip 处理 /clip 请求，将文本复制到剪贴板。
func handleClip(w http.ResponseWriter, r *http.Request) {
	encodedText := strings.TrimPrefix(r.URL.Path, "/clip/")
	if encodedText == "" {
		http.Error(w, "格式错误，请使用 /clip/<文本>", http.StatusBadRequest)
		return
	}
	textToCopy, err := url.PathUnescape(encodedText)
	if err != nil {
		log.Printf("错误: URL解码失败: %v", err)
		http.Error(w, "URL解码失败 ❌", http.StatusBadRequest)
		return
	}
	if err := clipboard.WriteAll(textToCopy); err != nil {
		log.Printf("错误: 写入剪贴板失败: %v", err)
		http.Error(w, "写入剪贴板失败 ❌", http.StatusInternalServerError)
		return
	}
	log.Printf("文本已复制到剪贴板: %s", textToCopy)
	_, runErr := ahk.RunScriptAndGetProcess("notify.ahk", textToCopy) // AHK通知脚本
	if runErr != nil {
		log.Printf("警告: 调用通知脚本失败: %v", runErr) // 通知失败通常不影响核心功能
	}
	fmt.Fprintf(w, "已复制到剪贴板 📋: %s\n", textToCopy) // 极简响应
}

// handleMonitorToggle 处理显示器电源切换请求
func handleMonitorToggle(w http.ResponseWriter, r *http.Request) {
	// 调用 winapi 包中的函数来切换显示器电源
	// newStateIsOff: true 表示执行后显示器推测为关闭，false 表示推测为开启
	newStateIsOff, err := winapi.ToggleMonitorPower()
	if err != nil {
		log.Printf("错误: 服务端执行切换显示器电源操作失败: %v", err)
		http.Error(w, "切换显示器电源失败 ❌", http.StatusInternalServerError)
		return
	}

	// 根据 winapi.ToggleMonitorPower 返回的推测新状态来构造响应
	if newStateIsOff {
		fmt.Fprintln(w, "已息屏 🌙") // 极简响应
	} else {
		fmt.Fprintln(w, "已亮屏 ☀️") // 极简响应
	}
}

// handleDebugPage 提供HTML调试页面
func handleDebugPage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(templateFS, "templates/debug.html")
	if err != nil {
		log.Printf("错误: 解析调试页面模板失败: %v", err)
		http.Error(w, "无法加载调试页面。", http.StatusInternalServerError)
		return
	}
	wsScheme := "ws"
	if r.TLS != nil { // 如果是通过HTTPS访问的，则WebSocket也用wss
		wsScheme = "wss"
	}
	// r.Host 包含了主机名和端口
	wsURL := fmt.Sprintf("%s://%s/ws/logs", wsScheme, r.Host)

	data := struct {
		WebSocketURL string
		InitialLogs  []string // 可以选择在这里预加载一些日志，但WebSocket会处理历史日志
	}{
		WebSocketURL: wsURL,
		InitialLogs:  []string{}, //让WebSocket连接后自行拉取或接收历史日志
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-f")
	err = tmpl.Execute(w, data)
	if err != nil {
		log.Printf("错误: 执行调试页面模板失败: %v", err)
		// http.Error 已经发送，这里只记录日志
	}
}

// serveWs 处理 WebSocket 连接请求
func serveWs(hub *logging.Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("错误: WebSocket连接升级失败: %v", err)
		return
	}
	hub.RegisterClient(conn) // 注册客户端到 Hub

	// 启动一个 goroutine 来处理从此客户端读取消息（如果需要双向通信）
	// 对于日志查看器，主要依赖服务器推送，客户端可能不需要发送太多消息
	// 但至少需要一个读取循环来检测连接是否关闭
	go func() {
		defer func() {
			hub.UnregisterClient(conn) // 确保在 goroutine 退出时注销客户端
			conn.Close()
		}()
		for {
			// 读取消息，但我们不期望客户端发送太多有用信息
			// 这个循环主要是为了检测连接关闭
			_, _, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("警告: WebSocket客户端 %s 意外断开: %v", conn.RemoteAddr(), err)
				}
				break // 发生任何读取错误都退出循环，触发defer中的注销
			}
		}
	}()
}
