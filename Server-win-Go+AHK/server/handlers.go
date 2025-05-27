// ************************************************************************
// ** 文件: server/handlers.go (UI和逻辑优化, 简化测试)                   **
// ** 描述: 实现 /setting 页面的 GET 和 POST 请求处理。                   **
// ** 主要改动：                                                     **
// ** - 适配 config.BarkConfig 中 NotifyOnSystemReady 字段。         **
// ** - 在 handleClip 中恢复对 URL 路径参数的解码。                    **
// ** - 移除 settings 页面对 DefaultTestTitle/Body 的处理。         **
// ************************************************************************
package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"bealinkserver/ahk"
	"bealinkserver/bark"
	"bealinkserver/config"
	"bealinkserver/logging"
	"bealinkserver/winapi"

	"github.com/atotto/clipboard"
	"github.com/gorilla/websocket"
)

//go:embed templates
var templateFS embed.FS

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}
	activeSleepProcess    *os.Process
	activeShutdownProcess *os.Process
	sleepMutex            sync.Mutex
	shutdownMutex         sync.Mutex
	settingsTemplate      *template.Template
	debugTemplate         *template.Template
)

func init() {
	log.Println("正在初始化 server 包的 HTML 模板...")
	var err error
	settingsTemplate, err = template.ParseFS(templateFS, "templates/settings.html")
	if err != nil {
		log.Fatalf("!!! 致命错误: 解析 settings.html 模板失败: %v。", err)
	}
	debugTemplate, err = template.ParseFS(templateFS, "templates/debug.html")
	if err != nil {
		log.Fatalf("!!! 致命错误: 解析 debug.html 模板失败: %v。", err)
	}
	log.Println("HTML 模板已成功解析并缓存。")
}

func getFormValueHelper(r *http.Request, key string) string {
	if r.MultipartForm != nil && r.MultipartForm.Value != nil {
		if values, ok := r.MultipartForm.Value[key]; ok && len(values) > 0 {
			return values[0]
		}
	}
	if r.Form != nil {
		if values, ok := r.Form[key]; ok && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

func clearProcess(p **os.Process, m *sync.Mutex, taskName string) { /* ... (代码同前) ... */
	if p == nil || *p == nil {
		return
	}
	processToWait := *p
	go func() {
		if processToWait == nil {
			return
		}
		pid := processToWait.Pid
		log.Printf("开始等待 %s AHK 脚本 (PID: %d) 结束...", taskName, pid)
		state, err := processToWait.Wait()
		if err != nil {
			log.Printf("等待 %s AHK 脚本 (PID: %d) 结束时发生错误: %v", taskName, pid, err)
		} else {
			log.Printf("%s AHK 脚本 (PID: %d) 已结束，退出状态: %s", taskName, pid, state.String())
		}
		m.Lock()
		if *p != nil && (*p).Pid == pid {
			*p = nil
			log.Printf("已清除活动的 %s 进程引用 (PID: %d)。", taskName, pid)
		} else {
			log.Printf("等待 %s (PID: %d) 结束，但全局引用已指向其他进程或为nil。", taskName, pid)
		}
		m.Unlock()
	}()
}

func handleRoot(w http.ResponseWriter, r *http.Request) { /* ... (代码同前) ... */
	if r.URL.Path == "/favicon.ico" {
		http.NotFound(w, r)
		return
	}
	hostname, _ := os.Hostname()
	localIP := getLocalIP()
	fmt.Fprintf(w, "Bealink Go 服务运行中。\n监听于: %s (或 http://localhost:%s)\n通过IP访问: http://%s:%s\n通过主机名(mDNS): http://%s.local:%s\n可用端点: /sleep, /shutdown, /clip/<text>, /getclip, /monitor, /ping, /debug, /setting",
		GlobalActualListenAddr, GlobalActualPort, localIP, GlobalActualPort, hostname, GlobalActualPort)
}
func handlePing(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "pong 🏓") }

func handleSleep(w http.ResponseWriter, r *http.Request) { /* ... (代码同前) ... */
	sleepMutex.Lock()
	defer sleepMutex.Unlock()
	if activeSleepProcess != nil {
		log.Printf("取消睡眠任务 (PID: %d)...", activeSleepProcess.Pid)
		if err := activeSleepProcess.Kill(); err != nil {
			log.Printf("错误: 取消睡眠任务失败: %v", err)
			http.Error(w, "取消睡眠任务失败", http.StatusInternalServerError)
			return
		}
		activeSleepProcess = nil
		fmt.Fprintln(w, "睡眠任务已取消 💤")
	} else {
		log.Println("启动睡眠倒计时...")
		process, err := ahk.RunScriptAndGetProcess("sleep_countdown.ahk")
		if err != nil {
			log.Printf("错误: 启动睡眠脚本失败: %v", err)
			http.Error(w, "启动睡眠脚本失败", http.StatusInternalServerError)
			return
		}
		activeSleepProcess = process
		go clearProcess(&activeSleepProcess, &sleepMutex, "睡眠")
		fmt.Fprintln(w, "睡眠倒计时已启动 😴")
	}
}
func handleShutdown(w http.ResponseWriter, r *http.Request) { /* ... (代码同前) ... */
	shutdownMutex.Lock()
	defer shutdownMutex.Unlock()
	if activeShutdownProcess != nil {
		log.Printf("取消关机任务 (PID: %d)...", activeShutdownProcess.Pid)
		if err := activeShutdownProcess.Kill(); err != nil {
			log.Printf("错误: 取消关机任务失败: %v", err)
			http.Error(w, "取消关机任务失败", http.StatusInternalServerError)
			return
		}
		activeShutdownProcess = nil
		fmt.Fprintln(w, "关机任务已取消 🚫")
	} else {
		log.Println("启动关机倒计时...")
		process, err := ahk.RunScriptAndGetProcess("shutdown_countdown.ahk")
		if err != nil {
			log.Printf("错误: 启动关机脚本失败: %v", err)
			http.Error(w, "启动关机脚本失败", http.StatusInternalServerError)
			return
		}
		activeShutdownProcess = process
		go clearProcess(&activeShutdownProcess, &shutdownMutex, "关机")
		fmt.Fprintln(w, "关机倒计时已启动 ⏳")
	}
}

func handleClip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		log.Printf("警告: /clip 收到非POST请求，方法: %s, 路径: %s, 来自: %s", r.Method, r.URL.Path, r.RemoteAddr)
		http.Error(w, "仅支持 POST 方法，且内容需为 JSON {\"content\":\"...\"}", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		log.Printf("警告: /clip 收到无法解析的JSON，来自: %s, 错误: %v", r.RemoteAddr, err)
		http.Error(w, "请求体需为合法 JSON 格式", http.StatusBadRequest)
		return
	}
	
	if req.Content == "" {
		log.Printf("提示: /clip 收到空剪贴板内容，来自: %s", r.RemoteAddr)
		http.Error(w, "剪贴板内容为空哦 ✨", http.StatusBadRequest)
		return
	}

	if err := clipboard.WriteAll(req.Content); err != nil {
		log.Printf("错误: 写入剪贴板失败: %v, 来自: %s", err, r.RemoteAddr)
		http.Error(w, "写入剪贴板失败 ❌", http.StatusInternalServerError)
		return
	}

	log.Printf("文本已复制到剪贴板: %s, 来自: %s", req.Content, r.RemoteAddr)
	if _, runErr := ahk.RunScriptAndGetProcess("notify.ahk", req.Content); runErr != nil {
		log.Printf("警告: 调用通知脚本失败: %v", runErr)
	}

	fmt.Fprintf(w, "已复制到剪贴板 📋: %s\n", req.Content)
}


func handleGetClip(w http.ResponseWriter, r *http.Request) {
	log.Printf("收到 getclip 请求，方法: %s, 路径: %s, 来自: %s", r.Method, r.URL.Path, r.RemoteAddr)
	if r.Method != http.MethodGet {
		http.Error(w, "仅支持 GET 方法", http.StatusMethodNotAllowed)
		return
	}
	clipboardContent, err := clipboard.ReadAll()
	if err != nil {
		log.Printf("错误: 读取剪贴板失败: %v, 来自: %s", err, r.RemoteAddr)
		if strings.Contains(err.Error(), "clipboard is empty") || strings.Contains(err.Error(), "format is not available") {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprintln(w, "")
			return
		}
		http.Error(w, "读取剪贴板内容失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, clipboardContent)
}
func handleMonitorToggle(w http.ResponseWriter, r *http.Request) { /* ... (代码同前) ... */
	newStateIsOff, err := winapi.ToggleMonitorPower()
	if err != nil {
		log.Printf("错误: 切换显示器电源失败: %v", err)
		http.Error(w, "切换显示器电源失败", http.StatusInternalServerError)
		return
	}
	if newStateIsOff {
		fmt.Fprintln(w, "已息屏 🌙")
	} else {
		fmt.Fprintln(w, "已亮屏 ☀️")
	}
}

func handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "仅支持 GET 方法", http.StatusMethodNotAllowed)
		return
	}
	currentCfg := config.GetConfig()
	useEncryption := false
	if currentCfg.EncryptionKey != "" && currentCfg.EncryptionIV != "" &&
		len(currentCfg.EncryptionKey) == 16 && len(currentCfg.EncryptionIV) == 16 {
		useEncryption = true
	}
	// 不再需要 DefaultTestTitle 和 DefaultTestBody
	templateData := struct {
		*config.BarkConfig
		UseEncryption bool
	}{BarkConfig: currentCfg, UseEncryption: useEncryption}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if settingsTemplate == nil {
		log.Println("错误: settings.html 模板尚未在 init() 中成功初始化。")
		http.Error(w, "服务器内部错误: 设置页面模板未加载。", http.StatusInternalServerError)
		return
	}
	if err := settingsTemplate.Execute(w, templateData); err != nil {
		log.Printf("错误: 执行 settings.html 模板失败: %v", err)
		if _, ok := w.(http.Flusher); !ok {
			http.Error(w, "渲染设置页面时发生内部错误。", http.StatusInternalServerError)
		}
	}
}

func handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST 方法", http.StatusMethodNotAllowed)
		return
	}
	log.Printf("调试: handleSaveSettings - 收到请求，Content-Type: %s", r.Header.Get("Content-Type"))
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		if err := r.ParseForm(); err != nil {
			log.Printf("错误: 解析表单数据失败: %v", err)
			http.Error(w, "无法解析表单数据。", http.StatusBadRequest)
			return
		}
		log.Println("调试: handleSaveSettings - 使用 r.ParseForm() 解析。")
	} else {
		log.Println("调试: handleSaveSettings - 使用 r.ParseMultipartForm() 解析成功。")
	}
	log.Println("调试: handleSaveSettings - 解析后 r.Form 内容:")
	for key, values := range r.Form {
		log.Printf("  r.Form -> %s: %v\n", key, values)
	}
	if r.MultipartForm != nil {
		log.Println("调试: handleSaveSettings - 解析后 r.MultipartForm.Value 内容:")
		for key, values := range r.MultipartForm.Value {
			log.Printf("  r.MultipartForm.Value -> %s: %v\n", key, values)
		}
	} else {
		log.Println("调试: handleSaveSettings - r.MultipartForm 为 nil。")
	}

	errUpdate := config.UpdateConfig(func(cfgToUpdate *config.BarkConfig) {
		cfgToUpdate.BarkFullURL = getFormValueHelper(r, "bark_full_url")
		cfgToUpdate.Group = getFormValueHelper(r, "group")
		cfgToUpdate.IconURL = getFormValueHelper(r, "icon_url")
		cfgToUpdate.Sound = getFormValueHelper(r, "sound")

		useEncryptionForm := getFormValueHelper(r, "use_encryption") == "on"
		if useEncryptionForm {
			cfgToUpdate.EncryptionKey = getFormValueHelper(r, "encryption_key")
			cfgToUpdate.EncryptionIV = getFormValueHelper(r, "encryption_iv")
			if len(cfgToUpdate.EncryptionKey) != 16 || len(cfgToUpdate.EncryptionIV) != 16 {
				log.Printf("警告: 用户提交的加密密钥或IV长度不为16。加密将不会启用。Key长度: %d, IV长度: %d", len(cfgToUpdate.EncryptionKey), len(cfgToUpdate.EncryptionIV))
			}
		} else {
			cfgToUpdate.EncryptionKey = ""
			cfgToUpdate.EncryptionIV = ""
		}

		cfgToUpdate.NotifyOnSystemReady = getFormValueHelper(r, "notify_on_system_ready") == "on"

		// 不再读取 DefaultTestTitle 和 DefaultTestBody
		// cfgToUpdate.DefaultTestTitle = getFormValueHelper(r, "default_test_title")
		// cfgToUpdate.DefaultTestBody = getFormValueHelper(r, "default_test_body")

		if valStr := getFormValueHelper(r, "retry_delay_sec"); valStr != "" {
			retryDelay, errRD := strconv.Atoi(valStr)
			if errRD == nil && retryDelay >= config.MinRetryInterval {
				cfgToUpdate.RetryDelaySec = retryDelay
			} else {
				log.Printf("警告: 无效的 RetryDelaySec 值 '%s'。保留原值 %d。", valStr, cfgToUpdate.RetryDelaySec)
			}
		} else {
			log.Printf("信息: 表单中未提供 RetryDelaySec，保留原值 %d。", cfgToUpdate.RetryDelaySec)
		}

		if valStr := getFormValueHelper(r, "max_retries"); valStr != "" {
			maxRetries, errMR := strconv.Atoi(valStr)
			if errMR == nil && maxRetries > 0 {
				cfgToUpdate.MaxRetries = maxRetries
			} else {
				log.Printf("警告: 无效的 MaxRetries 值 '%s'。保留原值 %d。", valStr, cfgToUpdate.MaxRetries)
			}
		} else {
			log.Printf("信息: 表单中未提供 MaxRetries，保留原值 %d。", cfgToUpdate.MaxRetries)
		}
	})
	if errUpdate != nil {
		log.Printf("错误: 保存配置失败: %v", errUpdate)
		http.Error(w, "保存配置失败。", http.StatusInternalServerError)
		return
	}
	log.Println("配置已成功更新并保存 (handleSaveSettings 返回前)。")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(w, "设置已成功保存！")
}

func handleTestBark(w http.ResponseWriter, r *http.Request) { /* ... (代码同前) ... */
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST 方法", http.StatusMethodNotAllowed)
		return
	}
	log.Println("收到测试 Bark 推送请求...")
	bark.GetNotifier().SendTestNotification() // SendTestNotification 内部将使用固定的测试内容
	currentCfg := config.GetConfig()
	sufficient, _, _, _, _, reason := bark.IsBarkConfigSufficient(currentCfg)
	if !sufficient {
		errMsg := fmt.Sprintf("测试通知可能无法发送，因为 Bark 配置不完整: %s", reason)
		log.Println(errMsg)
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(w, "测试通知已尝试发送。请检查你的 Bark App。")
}
func handleDebugPage(w http.ResponseWriter, r *http.Request) { /* ... (代码同前) ... */
	if debugTemplate == nil {
		log.Println("错误: debug.html 模板尚未在 init() 中成功初始化。")
		http.Error(w, "服务器内部错误: 调试页面模板未加载。", http.StatusInternalServerError)
		return
	}
	wsScheme := "ws"
	if r.TLS != nil {
		wsScheme = "wss"
	}
	wsURL := fmt.Sprintf("%s://%s/ws/logs", wsScheme, r.Host)
	data := struct{ WebSocketURL string }{WebSocketURL: wsURL}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := debugTemplate.Execute(w, data); err != nil {
		log.Printf("错误: 执行 debug.html 模板失败: %v", err)
	}
}
func serveWs(hub *logging.Hub, w http.ResponseWriter, r *http.Request) { /* ... (代码同前) ... */
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("错误: WebSocket连接升级失败: %v", err)
		return
	}
	hub.RegisterClient(conn)
	go func() {
		defer func() { hub.UnregisterClient(conn); conn.Close() }()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}()
}
