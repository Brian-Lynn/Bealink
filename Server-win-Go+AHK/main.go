package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"bealinkserver/ahk" // 确保 "bealinkserver" 是您 go.mod 中的模块名
	"bealinkserver/logging"
	"bealinkserver/server"
	"bealinkserver/winapi"

	"github.com/getlantern/systray"
)

const (
	iconFileName = "icon.ico"
	// 旧的 listenPort 常量已移除，端口处理由 preferredPorts 切片完成
)

var (
	originalLogOutput = os.Stderr
	actualServerAddr  string // 存储服务器实际监听的地址 (host:port)
)

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

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		return fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}
	log.Printf("尝试在浏览器中打开: %s", url)
	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("打开浏览器失败 (%s): %w", url, err)
	}
	return nil
}

func main() {
	logWriter := logging.Init(200)
	log.SetOutput(logWriter)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("程序启动...")

	_, findErr := ahk.FindAhkPath()
	if findErr != nil {
		log.Printf("警告: %v (AHK 相关功能可能不可用)", findErr)
	}

	systray.Run(onReady, onExit)
}

func onReady() {
	log.Println("系统托盘准备就绪。")

	// 定义期望的端口列表
	// 这是关键：server.Start 需要一个 []string 类型的参数
	preferredPorts := []string{":8088", ":8089", ":8090", ":8080"}

	coreServiceCtx, coreServiceCancel := context.WithCancel(context.Background())

	go logging.GetHub().Run(coreServiceCtx)

	// 启动HTTP服务并获取实际监听地址和是否使用了备用端口
	var errServerStart error
	var usedAlternativePort bool
	// 这是关键的调用：确保第二个参数是 preferredPorts (一个 []string)
	actualServerAddr, usedAlternativePort, errServerStart = server.Start(coreServiceCtx, preferredPorts, logging.GetHub())

	if errServerStart != nil {
		log.Fatalf("!!! 致命错误: HTTP服务启动失败: %v。程序将退出。", errServerStart)
		systray.SetIcon(loadIconBytes("icon.ico")) // 尝试使用默认图标或特定错误图标
		systray.SetTitle("Bealink Go - 错误")
		systray.SetTooltip(fmt.Sprintf("服务启动失败: %v", errServerStart))
		mErr := systray.AddMenuItem(fmt.Sprintf("错误: %v", errServerStart), "服务无法启动")
		mErr.Disable()
		mQuitErr := systray.AddMenuItem("退出", "关闭程序")
		go func() {
			<-mQuitErr.ClickedCh
			systray.Quit()
		}()
		return // onReady 提前结束
	}

	// 如果使用了备用端口，则自动打开浏览器到调试页面
	if usedAlternativePort {
		log.Printf("服务已在备用地址 %s 上启动，将自动打开调试页面。", actualServerAddr)
		parts := strings.Split(actualServerAddr, ":")
		portStr := parts[len(parts)-1]
		debugURL := fmt.Sprintf("http://localhost:%s/debug", portStr)
		errOpen := openBrowser(debugURL)
		if errOpen != nil {
			log.Printf("警告: 自动打开调试日志页面失败: %v", errOpen)
		}
	}

	systray.SetIcon(loadIconBytes(iconFileName))
	systray.SetTitle("Bealink Go 服务")
	systray.SetTooltip(fmt.Sprintf("Bealink Go (监听于 %s)", actualServerAddr))

	mViewLogs := systray.AddMenuItem("查看调试日志", "在浏览器中打开调试日志页面")
	mAutoStart := systray.AddMenuItem("开机自启", "设置/取消开机自启")
	autoStartEnabled, err := winapi.IsAutoStartEnabled()
	if err == nil && autoStartEnabled {
		mAutoStart.Check()
	} else if err != nil {
		log.Printf("错误: 检查开机自启状态失败: %v", err)
		mAutoStart.Disable()
	}

	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "关闭服务")

	go func() {
		for {
			select {
			case <-mViewLogs.ClickedCh:
				if actualServerAddr == "" {
					log.Println("错误: 服务器地址未知，无法打开调试页面。")
					continue
				}
				parts := strings.Split(actualServerAddr, ":")
				portStr := parts[len(parts)-1]
				debugURL := fmt.Sprintf("http://localhost:%s/debug", portStr)
				err := openBrowser(debugURL)
				if err != nil {
					log.Printf("错误: 打开调试日志页面失败: %v", err)
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
				log.Println("收到退出请求...")
				coreServiceCancel()
				time.Sleep(1 * time.Second)
				systray.Quit()
				return
			case <-coreServiceCtx.Done():
				log.Println("核心服务上下文已取消，准备退出托盘。")
				return
			}
		}
	}()
	log.Println("onReady 执行完毕。")
}

func onExit() {
	log.Println("程序正在退出...")
}
