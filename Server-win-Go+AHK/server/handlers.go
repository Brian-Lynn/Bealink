package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"bealinkserver/ahk"
	"bealinkserver/bark"
	"bealinkserver/logging"
	"bealinkserver/winapi"
	"html/template"

	"github.com/atotto/clipboard"
	"github.com/gorilla/websocket"
)

//go:embed html
var templateFS embed.FS

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}
)

var (
	activeSleepProcess    *os.Process
	activeShutdownProcess *os.Process
	sleepMutex            sync.Mutex
	shutdownMutex         sync.Mutex
)

func clearProcess(p **os.Process, m *sync.Mutex, taskName string) {
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

// handleMobileUI 处理移动端控制台请求 (GET /)
func handleMobileUI(w http.ResponseWriter, r *http.Request) {
	// 记录设备连接
	log.Printf("设备连接: %s (User-Agent: %s)", r.RemoteAddr, r.UserAgent())

	// 使用 embed.FS 读取 HTML 文件，不依赖工作目录
	htmlContent, err := templateFS.ReadFile("html/mobile_ui.html")
	if err != nil {
		log.Printf("错误: 从 embed.FS 读取 mobile_ui.html 失败: %v", err)
		http.Error(w, "404 Not Found: UI File Missing", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(htmlContent)
}

func handleRoot(w http.ResponseWriter, r *http.Request) { handleMobileUI(w, r) }
func handlePing(w http.ResponseWriter, r *http.Request) { w.Write([]byte("pong")) }

// 剪贴板接口
func handleClip(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		if text == "" {
			text = r.FormValue("text")
		}
		if text != "" {
			clipboard.WriteAll(text)
			log.Printf("剪切板写入 (来自 %s): %d bytes", r.RemoteAddr, len(text))
			w.Write([]byte("ok"))
			return
		}
		http.Error(w, "Empty body", http.StatusBadRequest)
	case http.MethodGet:
		content, _ := clipboard.ReadAll()
		log.Printf("剪切板读取 (来自 %s): %d bytes", r.RemoteAddr, len(content))
		w.Write([]byte(content))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleGetClip(w http.ResponseWriter, r *http.Request) {
	content, _ := clipboard.ReadAll()
	log.Printf("handleGetClip 请求来自 %s, 内容长度: %d", r.RemoteAddr, len(content))
	w.Write([]byte(content))
}

// 媒体与音量
func handleMediaInfo(w http.ResponseWriter, r *http.Request) {
	info := GetMediaInfo()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func handleVolumeInfo(w http.ResponseWriter, r *http.Request) {
	vol := GetVolume()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"volume": vol})
}

func handleVolumeSet(w http.ResponseWriter, r *http.Request) {
	valStr := r.URL.Query().Get("val")
	val, err := strconv.Atoi(valStr)
	if err != nil {
		http.Error(w, "Invalid volume", http.StatusBadRequest)
		return
	}
	SetVolume(val)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleMute(w http.ResponseWriter, r *http.Request) {
	ToggleMute()
	w.WriteHeader(http.StatusOK)
}

// 文本与粘贴
func handleText(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	text := r.FormValue("text")
	if text == "" {
		bodyBytes, _ := io.ReadAll(r.Body)
		text = string(bodyBytes)
	}
	if text != "" {
		clipboard.WriteAll(text)
		go func() {
			time.Sleep(100 * time.Millisecond)
			winapi.Paste()
		}()
		log.Printf("已接收文本并粘贴: %s...", limitStr(text, 20))
	}
	w.Write([]byte("Sent"))
}

func handlePaste(w http.ResponseWriter, r *http.Request) {
	winapi.Paste()
	w.Write([]byte("Pasted"))
}

// 系统控制
func handleMediaControl(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimPrefix(r.URL.Path, "/media/")
	switch action {
	case "play", "playpause":
		winapi.MediaPlayPause()
	case "prev":
		winapi.MediaPrev()
	case "next":
		winapi.MediaNext()
	}
	w.WriteHeader(http.StatusOK)
}

func handleMonitorOff(w http.ResponseWriter, r *http.Request) {
	// 如果当前推测显示器是开启的 (GetState返回false)，则切换电源以关闭
	if !winapi.GetCurrentMonitorState() {
		winapi.ToggleMonitorPower()
		w.Write([]byte("Monitor Off Sent"))
	} else {
		w.Write([]byte("Monitor Already Off (Assumed)"))
	}
}

func handleSleep(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	sleepMutex.Lock()
	defer sleepMutex.Unlock()
	if activeSleepProcess != nil {
		log.Printf("取消睡眠任务 (PID: %d)...", activeSleepProcess.Pid)
		if err := activeSleepProcess.Kill(); err != nil {
			log.Printf("错误: 取消睡眠任务失败: %v", err)
			http.Error(w, "cancel_error", http.StatusInternalServerError)
			return
		}
		activeSleepProcess = nil
		log.Println("睡眠任务已取消 (由前端请求)")
		w.Write([]byte(`{"status":"cancelled"}`))
		return
	}

	proc, err := ahk.RunScriptAndGetProcess("sleep_countdown.ahk")
	if err != nil {
		// 回退：直接调用系统睡眠
		cmdStr := `Run, rundll32.exe powrprof.dll,SetSuspendState 0,1,0`
		_, err2 := ahk.RunAhkCode(cmdStr)
		if err2 != nil {
			log.Printf("睡眠指令失败: %v", err2)
			http.Error(w, "Error", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"status":"executed"}`))
		log.Println("Info: sleep executed via fallback AHK code")
		return
	}
	activeSleepProcess = proc
	go clearProcess(&activeSleepProcess, &sleepMutex, "睡眠")
	scriptDir := filepath.Dir(os.Args[0])
	scriptFullPath := filepath.Join(scriptDir, "ahk", "script", "sleep_countdown.ahk")
	dur := ahk.GetScriptCountdownSeconds(scriptFullPath)
	w.Write([]byte(fmt.Sprintf(`{"status":"started","duration":%d}`, dur)))

}

func handleShutdown(w http.ResponseWriter, r *http.Request) {
	shutdownMutex.Lock()
	defer shutdownMutex.Unlock()
	if activeShutdownProcess != nil {
		log.Printf("取消关机任务 (PID: %d)...", activeShutdownProcess.Pid)
		if err := activeShutdownProcess.Kill(); err != nil {
			log.Printf("错误: 取消关机任务失败: %v", err)
			http.Error(w, "cancel_error", http.StatusInternalServerError)
			return
		}
		activeShutdownProcess = nil
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"cancelled"}`))
		return
	}

	proc, err := ahk.RunScriptAndGetProcess("shutdown_countdown.ahk")
	if err != nil {
		// 回退：直接关机
		exec.Command("shutdown", "/s", "/t", "0").Run()
		w.Write([]byte(`{"status":"executed"}`))
		log.Println("Info: shutdown executed via direct system call (fallback)")
		return
	}
	activeShutdownProcess = proc
	go clearProcess(&activeShutdownProcess, &shutdownMutex, "关机")
	scriptDir := filepath.Dir(os.Args[0])
	scriptFullPath := filepath.Join(scriptDir, "ahk", "script", "shutdown_countdown.ahk")
	dur := ahk.GetScriptCountdownSeconds(scriptFullPath)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(fmt.Sprintf(`{"status":"started","duration":%d}`, dur)))
}

// 图片上传
func handleUploadImage(w http.ResponseWriter, r *http.Request) {
	file, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "File error", http.StatusBadRequest)
		return
	}
	defer file.Close()

	tempDir := os.TempDir()
	tempFile := filepath.Join(tempDir, "bealink_clip.png")

	f, err := os.Create(tempFile)
	if err != nil {
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	io.Copy(f, file)

	data, err := os.ReadFile(tempFile)
	if err != nil {
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}
	if err := winapi.SetClipboardImage(data, filepath.Base(tempFile)); err != nil {
		log.Printf("设置剪贴板图片失败: %v", err)
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}
	w.Write([]byte("Image copied"))
}

func limitStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// 设置与调试
func handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	cfg := bark.GetConfig()

	// 读取模板文件
	tmplData, err := templateFS.ReadFile("html/settings.html")
	if err != nil {
		log.Printf("读取设置页面模板失败: %v", err)
		http.Error(w, "模板文件未找到", http.StatusInternalServerError)
		return
	}

	// 解析模板
	tmpl, err := template.New("settings").Parse(string(tmplData))
	if err != nil {
		log.Printf("解析设置页面模板失败: %v", err)
		http.Error(w, "模板解析失败", http.StatusInternalServerError)
		return
	}

	// 准备模板数据
	type SettingsData struct {
		BarkFullURL         string
		Sound               string
		SoundOptions        []string
		EncryptionKey       string
		EncryptionIV        string
		UseEncryption       bool
		NotifyOnSystemReady bool
	}

	data := SettingsData{
		BarkFullURL:         cfg.BarkFullURL,
		Sound:               cfg.Sound,
		SoundOptions:        bark.SoundOptions,
		EncryptionKey:       cfg.EncryptionKey,
		EncryptionIV:        cfg.EncryptionIV,
		UseEncryption:       cfg.EncryptionKey != "" && cfg.EncryptionIV != "" && len(cfg.EncryptionKey) == 16 && len(cfg.EncryptionIV) == 16,
		NotifyOnSystemReady: cfg.NotifyOnSystemReady,
	}

	// 渲染模板
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("渲染设置页面模板失败: %v", err)
		http.Error(w, "模板渲染失败", http.StatusInternalServerError)
		return
	}
}

func handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	// 支持 JSON 和 FormData 两种格式
	var m map[string]interface{}

	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		// JSON 格式
		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 {
			http.Error(w, "empty body", http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(body, &m); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
	} else {
		// FormData 格式（包括 multipart/form-data 和 application/x-www-form-urlencoded）
		// 先尝试解析 multipart form（最大 32MB）
		if strings.HasPrefix(contentType, "multipart/form-data") {
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				log.Printf("错误: 解析 multipart form 失败: %v", err)
				http.Error(w, "parse multipart form error: "+err.Error(), http.StatusBadRequest)
				return
			}
		} else {
			// application/x-www-form-urlencoded
			if err := r.ParseForm(); err != nil {
				log.Printf("错误: 解析 form 失败: %v", err)
				http.Error(w, "parse form error: "+err.Error(), http.StatusBadRequest)
				return
			}
		}

		m = make(map[string]interface{})
		// 使用 PostFormValue 来获取 POST 数据（支持 multipart 和 urlencoded）
		// 读取所有字段，包括空字符串（允许清空配置）
		barkURL := r.PostFormValue("bark_full_url")
		log.Printf("调试: 从表单接收到的 bark_full_url: '%s'", barkURL)
		m["bark_full_url"] = barkURL
		m["sound"] = r.PostFormValue("sound")
		m["encryption_key"] = r.PostFormValue("encryption_key")
		m["encryption_iv"] = r.PostFormValue("encryption_iv")
		// checkbox 处理：如果表单中有该字段且值为 "on"，则为 true，否则为 false
		if r.PostFormValue("notify_on_system_ready") == "on" {
			m["notify_on_system_ready"] = true
		} else {
			m["notify_on_system_ready"] = false
		}
	}

	log.Printf("调试: handleSaveSettings - 准备更新的配置数据: %+v", m)

	err := bark.UpdateConfig(func(cfg *bark.BarkConfig) {
		// 更新所有字段，包括空字符串（允许清空配置）
		if v, ok := m["bark_full_url"].(string); ok {
			log.Printf("调试: 更新 BarkFullURL 从 '%s' 到 '%s'", cfg.BarkFullURL, v)
			cfg.BarkFullURL = v
		}
		if v, ok := m["sound"].(string); ok {
			cfg.Sound = v
		}
		if v, ok := m["encryption_key"].(string); ok {
			cfg.EncryptionKey = v
		}
		if v, ok := m["encryption_iv"].(string); ok {
			cfg.EncryptionIV = v
		}
		if v, ok := m["notify_on_system_ready"].(bool); ok {
			cfg.NotifyOnSystemReady = v
		}
	})
	if err != nil {
		log.Printf("错误: 保存配置失败: %v", err)
		http.Error(w, "保存配置失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 验证保存后的配置
	savedCfg := bark.GetConfig()
	log.Printf("调试: handleSaveSettings - 保存后的配置 BarkFullURL: '%s'", savedCfg.BarkFullURL)

	w.Write([]byte("设置已成功保存！"))
}

func handleDebugPage(w http.ResponseWriter, r *http.Request) {
	// 构建 WebSocket URL
	wsScheme := "ws"
	if r.TLS != nil {
		wsScheme = "wss"
	}
	wsURL := fmt.Sprintf("%s://%s/ws/logs", wsScheme, r.Host)

	// 读取模板文件
	tmplData, err := templateFS.ReadFile("html/debug.html")
	if err != nil {
		log.Printf("读取调试页面模板失败: %v", err)
		http.Error(w, "模板文件未找到", http.StatusInternalServerError)
		return
	}

	// 解析模板
	tmpl, err := template.New("debug").Parse(string(tmplData))
	if err != nil {
		log.Printf("解析调试页面模板失败: %v", err)
		http.Error(w, "模板解析失败", http.StatusInternalServerError)
		return
	}

	// 准备模板数据
	type DebugData struct {
		WebSocketURL string
	}

	data := DebugData{
		WebSocketURL: wsURL,
	}

	// 渲染模板
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("渲染调试页面模板失败: %v", err)
		http.Error(w, "模板渲染失败", http.StatusInternalServerError)
		return
	}
}
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Not implemented yet", 501)
}

func handleMonitorToggle(w http.ResponseWriter, r *http.Request) {
	newState, err := winapi.ToggleMonitorPower()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if newState {
		w.Write([]byte("off"))
	} else {
		w.Write([]byte("on"))
	}
}
func handleVolumeUp(w http.ResponseWriter, r *http.Request) {
	_ = winapi.VolumeUp()
	w.Write([]byte("ok"))
}
func handleVolumeDown(w http.ResponseWriter, r *http.Request) {
	_ = winapi.VolumeDown()
	w.Write([]byte("ok"))
}
func handleMediaPlayPause(w http.ResponseWriter, r *http.Request) {
	// 使用简单快捷键: Ctrl+Alt+P 作为播放/暂停
	if err := winapi.SendKeyWithModifiers(true, true, false, 0x50); err != nil {
		log.Printf("发送播放/暂停快捷键失败: %v", err)
	}
	w.Write([]byte("ok"))
}
func handleMediaNext(w http.ResponseWriter, r *http.Request) {
	// 使用快捷键: Ctrl+Alt+Right 作为下一首
	if err := winapi.SendKeyWithModifiers(true, true, false, winapi.VK_RIGHT); err != nil {
		log.Printf("发送下一首快捷键失败: %v", err)
	}
	w.Write([]byte("ok"))
}

func handleMediaPrev(w http.ResponseWriter, r *http.Request) {
	// 使用快捷键: Ctrl+Alt+Left 作为上一首
	if err := winapi.SendKeyWithModifiers(true, true, false, winapi.VK_LEFT); err != nil {
		log.Printf("发送上一首快捷键失败: %v", err)
	}
	w.Write([]byte("ok"))
}

func serveWs(hub *logging.Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("升级 WebSocket 失败: %v", err)
		return
	}
	hub.RegisterClient(conn)
	go func() {
		defer hub.UnregisterClient(conn)
		for {
			if _, _, err := conn.NextReader(); err != nil {
				return
			}
		}
	}()
}

func handleTestBark(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 检查配置是否有效
	cfg := bark.GetConfig()
	sufficient, _, _, _, _, reason := bark.IsBarkConfigSufficient(cfg)
	if !sufficient {
		log.Printf("错误: Bark 配置不完整，无法发送测试通知: %s", reason)
		http.Error(w, fmt.Sprintf(`{"status":"error","message":"Bark 配置不完整: %s"}`, reason), http.StatusBadRequest)
		return
	}

	// 获取当前选择的铃声（如果有提供）
	selectedSound := r.FormValue("sound")
	if selectedSound != "" {
		cfg.Sound = selectedSound
	}

	// 发送测试推送通知（使用指定或已保存的铃声）
	bark.NotifyEventWithConfig("test", cfg)

	// 返回成功响应
	w.Write([]byte(`{"status":"ok","message":"测试通知已发送，请检查你的 Bark App"}`))
}

// getIconPath 返回深色图标文件路径
func getIconPath() string {
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("警告: 获取可执行文件路径失败: %v", err)
		return ""
	}
	return filepath.Join(filepath.Dir(exePath), "assets", "dark.ico")
}

// handleFavicon 返回深色图标
func handleFavicon(w http.ResponseWriter, r *http.Request) {
	iconPath := getIconPath()
	if iconPath == "" {
		http.Error(w, "无法确定图标路径", http.StatusInternalServerError)
		return
	}

	iconData, err := os.ReadFile(iconPath)
	if err != nil {
		log.Printf("错误: 读取图标文件失败 %s: %v", iconPath, err)
		http.Error(w, "图标文件未找到", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "image/x-icon")
	w.Write(iconData)
}

// handleIconICO 返回深色图标
func handleIconICO(w http.ResponseWriter, r *http.Request) {
	iconPath := getIconPath()
	if iconPath == "" {
		http.Error(w, "无法确定图标路径", http.StatusInternalServerError)
		return
	}

	iconData, err := os.ReadFile(iconPath)
	if err != nil {
		log.Printf("错误: 读取图标文件失败 %s: %v", iconPath, err)
		http.Error(w, "图标文件未找到", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "image/x-icon")
	w.Write(iconData)
}
