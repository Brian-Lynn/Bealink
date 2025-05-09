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

	"bealinkserver/logging" // 确保替换为您的模块名

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

	// GlobalActualListenAddr 和 GlobalActualPort 将存储服务器实际监听的地址和端口
	// 供 handleRoot 使用
	GlobalActualListenAddr string
	GlobalActualPort       string
)

func getLocalIP() string { // 这个函数现在也在这里，可以被 handleRoot 直接使用
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

func Start(ctx context.Context, preferredPorts []string, logHub *logging.Hub) (actualListenAddr string, usedAlternativePort bool, err error) {
	log.Println("核心服务 (HTTP, mDNS) 启动中...")

	var listener net.Listener
	var currentListenPort string // like ":8088"

	for i, portSpec := range preferredPorts {
		currentListenPort = portSpec
		log.Printf("尝试在端口 %s 上启动HTTP服务...", currentListenPort)
		tempListener, listenErr := net.Listen("tcp", currentListenPort)
		if listenErr == nil {
			listener = tempListener
			if i > 0 {
				usedAlternativePort = true
			}
			log.Printf("HTTP服务成功在端口 %s 上监听。", currentListenPort)
			break
		}
		if opErr, ok := listenErr.(*net.OpError); ok {
			if sysErr, ok := opErr.Err.(*os.SyscallError); ok {
				if strings.Contains(sysErr.Error(), "address already in use") || strings.Contains(sysErr.Error(), "Only one usage of each socket address") {
					log.Printf("警告: 端口 %s 已被占用。", currentListenPort)
					if i == len(preferredPorts)-1 {
						return "", false, fmt.Errorf("所有尝试的端口 (%v) 都已被占用: %w", preferredPorts, listenErr)
					}
					continue
				}
			}
		}
		return "", false, fmt.Errorf("HTTP服务在端口 %s 上监听失败: %w", currentListenPort, listenErr)
	}

	if listener == nil {
		return "", false, fmt.Errorf("未能成功在任何指定端口上监听: %v", preferredPorts)
	}

	GlobalActualListenAddr = listener.Addr().String() // e.g., "[::]:8088" or "0.0.0.0:8088"
	parts := strings.Split(GlobalActualListenAddr, ":")
	GlobalActualPort = parts[len(parts)-1] // Just the port number, e.g., "8088"

	portInt, _ := strconv.Atoi(GlobalActualPort)
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "BealinkGoHost"
	}
	var mDNSErr error
	mDNSServer, mDNSErr = zeroconf.Register(hostname, mDNSServiceType, mDNSDomain, portInt, []string{"path=/"}, nil)
	if mDNSErr != nil {
		log.Printf("警告: mDNS 服务注册失败: %v", mDNSErr)
	} else {
		log.Printf("mDNS 服务注册成功，指向端口 %d。", portInt)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoot) // handleRoot can now use GlobalActualListenAddr and GlobalActualPort
	mux.HandleFunc("/ping", handlePing)
	mux.HandleFunc("/sleep", handleSleep)
	mux.HandleFunc("/shutdown", handleShutdown)
	mux.HandleFunc("/clip/", handleClip)
	mux.HandleFunc("/monitor", handleMonitorToggle)
	mux.HandleFunc("/debug", handleDebugPage)
	mux.HandleFunc("/ws/logs", func(w http.ResponseWriter, r *http.Request) {
		serveWs(logHub, w, r)
	})

	httpServer = &http.Server{Handler: mux}

	log.Printf("HTTP 服务实际监听于: %s (本机IP可能为: http://%s:%s)", GlobalActualListenAddr, getLocalIP(), GlobalActualPort)
	if mDNSServer != nil {
		log.Printf("mDNS 可访问地址: http://%s.%s:%d", hostname, mDNSDomain, portInt)
	}

	httpServerErrChan := make(chan error, 1)
	go func() {
		err := httpServer.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP 服务器监听 goroutine 意外结束: %v", err)
		} else {
			log.Println("HTTP 服务器监听 goroutine 正常结束。")
		}
		httpServerErrChan <- err
		close(httpServerErrChan)
	}()

	go func() {
		select {
		case err := <-httpServerErrChan:
			if err != nil && err != http.ErrServerClosed {
				log.Printf("!!! HTTP 服务器错误: %v", err)
			}
		case <-ctx.Done():
			log.Println("收到退出信号，开始关闭HTTP和mDNS服务...")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				log.Printf("HTTP 服务器关闭错误: %v", err)
			} else {
				log.Println("HTTP 服务器已关闭。")
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
