// ************************************************************************
// ** 文件: server/server.go (模板初始化调整)                             **
// ** 描述: 注册 /setting 页面的路由。模板初始化移至 handlers.go 的 init()。**
// ************************************************************************
package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"bealinkserver/logging" // 假设这是你项目中的包

	"github.com/grandcat/zeroconf"
)

const (
	mDNSServiceName = "BealinkGo"
	mDNSServiceType = "_http._tcp"
	mDNSDomain      = "local."
)

var (
	httpServer *http.Server
	mDNSServer *zeroconf.Server

	GlobalActualListenAddr string
	GlobalActualPort       string
)

// getLocalIP 仍然保留，以防项目其他地方用到，但在此次日志优化中，其直接调用被 getLocalIPv4s 替代。
func getLocalIP() string {
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

// getLocalIPv4s 获取本机所有非环回的、单播 IPv4 地址。
func getLocalIPv4s() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Printf("警告: 获取本机网络接口地址失败: %v", err)
		return ips // 返回空切片
	}

	for _, address := range addrs {
		// 检查地址是否为 IPNet 类型
		if ipnet, ok := address.(*net.IPNet); ok &&
			!ipnet.IP.IsLoopback() && // 排除环回地址
			ipnet.IP.To4() != nil { // 确保是 IPv4 地址
			ips = append(ips, ipnet.IP.String())
		}
	}
	return ips
}

func Start(ctx context.Context, preferredPorts []string, logHub *logging.Hub) (actualListenAddr string, usedAlternativePort bool, err error) {
	log.Println("核心服务 (HTTP, mDNS) 启动中...")
	// initTemplates() // 不再在此处调用，已移至 handlers.go 的包级别 init() 函数

	var listener net.Listener
	var currentListenPort string // 当前尝试监听的端口字符串，可能包含 ":" 前缀

	for i, portSpec := range preferredPorts {
		currentListenPort = portSpec // 例如 ":8080" 或 "8080"
		log.Printf("尝试在端口 %s 上启动HTTP服务...", strings.TrimPrefix(currentListenPort, ":"))
		tempListener, listenErr := net.Listen("tcp", currentListenPort)
		if listenErr == nil {
			listener = tempListener
			if i > 0 {
				usedAlternativePort = true
			}
			// 根据要求修改日志格式
			log.Printf("HTTP 服务已在端口 %s 上成功启动监听。", strings.TrimPrefix(currentListenPort, ":"))
			break
		}
		if opErr, ok := listenErr.(*net.OpError); ok {
			if sysErr, ok := opErr.Err.(*os.SyscallError); ok {
				if strings.Contains(sysErr.Error(), "address already in use") || strings.Contains(sysErr.Error(), "Only one usage of each socket address") {
					log.Printf("警告: 端口 %s 已被占用。", strings.TrimPrefix(currentListenPort, ":"))
					if i == len(preferredPorts)-1 {
						return "", false, fmt.Errorf("所有尝试的端口 (%v) 都已被占用: %w", preferredPorts, listenErr)
					}
					continue
				}
			}
		}
		return "", false, fmt.Errorf("HTTP服务在端口 %s 上监听失败: %w", strings.TrimPrefix(currentListenPort, ":"), listenErr)
	}

	if listener == nil {
		return "", false, fmt.Errorf("未能成功在任何指定端口上监听: %v", preferredPorts)
	}

	GlobalActualListenAddr = listener.Addr().String() // 例如 "[::]:8088"
	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		log.Printf("警告: 从监听地址 %s 解析端口失败: %v", listener.Addr().String(), err)
		// 回退逻辑，尝试从 currentListenPort 获取端口号
		if strings.HasPrefix(currentListenPort, ":") {
			GlobalActualPort = strings.TrimPrefix(currentListenPort, ":")
		} else {
			GlobalActualPort = currentListenPort // 如果 currentListenPort 就是 "8080"
		}
		if GlobalActualPort == "" { // 最后的保障
			GlobalActualPort = "未知"
		}
	} else {
		GlobalActualPort = portStr // 例如 "8088"
	}

	portInt, convErr := strconv.Atoi(GlobalActualPort)
	if convErr != nil {
		log.Printf("警告: 转换端口号 '%s' 为整数失败: %v。mDNS注册可能会使用默认值或失败。", GlobalActualPort, convErr)
		// 可以选择一个默认端口或者不注册mDNS
		portInt = 0 // 或者一个常用的默认值，但0通常表示由系统选择，可能不适合mDNS
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "BealinkGoHost" // 默认主机名
	}

	if portInt > 0 { // 仅当端口有效时注册mDNS
		var mDNSErr error
		mDNSServer, mDNSErr = zeroconf.Register(hostname, mDNSServiceType, mDNSDomain, portInt, []string{"path=/"}, nil)
		if mDNSErr != nil {
			log.Printf("警告: mDNS 服务注册失败: %v", mDNSErr)
		} else {
			log.Printf("mDNS 服务注册成功 (%s)，主机指向端口 %d。", hostname, portInt)
		}
	} else {
		log.Printf("警告: 端口号无效 (%s)，跳过mDNS服务注册。", GlobalActualPort)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("/ping", handlePing)
	mux.HandleFunc("/sleep", handleSleep)
	mux.HandleFunc("/shutdown", handleShutdown)
	mux.HandleFunc("/clip", handleClip) // 兼容 /clip 和 /clip/
	mux.HandleFunc("/clip/", handleClip)
	mux.HandleFunc("/getclip", handleGetClip)
	mux.HandleFunc("/monitor", handleMonitorToggle)
	mux.HandleFunc("/volume/up", handleVolumeUp)
	mux.HandleFunc("/volume/down", handleVolumeDown)
	mux.HandleFunc("/volume/info", handleVolumeInfo)
	mux.HandleFunc("/media/playpause", handleMediaPlayPause)
	mux.HandleFunc("/media/play", handleMediaPlayPause)
	mux.HandleFunc("/media/info", handleMediaInfo)
	mux.HandleFunc("/volume/set", handleVolumeSet)
	mux.HandleFunc("/media/next", handleMediaNext)
	mux.HandleFunc("/media/prev", handleMediaPrev)
	mux.HandleFunc("/text", handleText)
	mux.HandleFunc("/paste", handlePaste)
	mux.HandleFunc("/upload/image", handleUploadImage)
	mux.HandleFunc("/debug", handleDebugPage)
	mux.HandleFunc("/ws/logs", func(w http.ResponseWriter, r *http.Request) { serveWs(logHub, w, r) })
	mux.HandleFunc("/setting", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handleSettingsPage(w, r)
		} else if r.Method == http.MethodPost {
			handleSaveSettings(w, r)
		} else {
			http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/test_bark", handleTestBark)
	mux.HandleFunc("/favicon.ico", handleFavicon)
	mux.HandleFunc("/icon.ico", handleIconICO)

	httpServer = &http.Server{Handler: mux, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}

	// 根据要求修改日志格式
	log.Printf("HTTP 服务实际监听于端口: %s", GlobalActualPort)

	// 列出本机可访问的 IPv4 地址
	localIPv4s := getLocalIPv4s()
	if len(localIPv4s) > 0 {
		log.Println("本机可访问的 IPv4 地址列表:")
		for _, ip := range localIPv4s {
			log.Printf("  - http://%s:%s", ip, GlobalActualPort)
		}
	} else {
		log.Println("未能获取到本机可访问的 IPv4 地址。")
	}

	if mDNSServer != nil { // 保持 mDNS 可访问地址的日志
		log.Printf("mDNS 可访问地址: http://%s.%s:%s", hostname, mDNSDomain, GlobalActualPort)
	}

	httpServerErrChan := make(chan error, 1)
	go func() {
		// 根据要求修改日志格式
		log.Printf("HTTP 服务开始在端口 %s 上提供服务...", GlobalActualPort)
		errServe := httpServer.Serve(listener)
		if errServe != nil && errServe != http.ErrServerClosed {
			log.Printf("HTTP 服务器监听 goroutine 意外结束: %v", errServe)
		} else {
			log.Println("HTTP 服务器监听 goroutine 正常结束。")
		}
		httpServerErrChan <- errServe
		close(httpServerErrChan)
	}()

	go func() {
		select {
		case errFromServe := <-httpServerErrChan:
			if errFromServe != nil && errFromServe != http.ErrServerClosed {
				log.Printf("!!! HTTP 服务器错误，服务可能已停止: %v", errFromServe)
			}
		case <-ctx.Done():
			log.Println("收到退出信号，开始关闭HTTP和mDNS服务...")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				log.Printf("HTTP 服务器关闭错误: %v", err)
			} else {
				log.Println("HTTP 服务器已优雅关闭。")
			}
			if mDNSServer != nil {
				mDNSServer.Shutdown()
				log.Println("mDNS 服务已注销。")
			}
			log.Println("核心服务 (HTTP, mDNS) 已停止。")
		}
	}()
	return GlobalActualListenAddr, usedAlternativePort, nil
}
