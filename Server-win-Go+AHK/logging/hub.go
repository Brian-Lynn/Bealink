package logging

import (
	"context"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

// Hub 维护一组活动的 WebSocket 客户端，并向它们广播消息。
type Hub struct {
	clients    map[*websocket.Conn]bool // 注册的客户端
	broadcast  chan []byte              // 从日志系统传入的消息（已弃用，不再使用）
	register   chan *websocket.Conn     // 注册请求
	unregister chan *websocket.Conn     // 注销请求
	mu         sync.Mutex               // 用于保护 clients map
}

// NewHub 创建一个新的 Hub。
func NewHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte, 16),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		clients:    make(map[*websocket.Conn]bool),
	}
}

// Run 启动 Hub 的主循环。
// 它应该在一个单独的 goroutine 中运行。
// ctx 用于控制 Hub 的生命周期。
func (h *Hub) Run(ctx context.Context) {
	log.Println("WebSocket 日志推送中心 (Hub) 启动...")
	defer log.Println("WebSocket 日志推送中心 (Hub) 已停止。")

	for {
		select {
		case <-ctx.Done(): // 上下文取消，Hub 停止
			h.mu.Lock()
			for client := range h.clients {
				// 尝试优雅关闭客户端连接
				client.WriteMessage(websocket.CloseMessage, []byte{})
				delete(h.clients, client)
				client.Close()
			}
			h.mu.Unlock()
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			log.Printf("WebSocket 客户端已连接: %s. 当前客户端数量: %d", client.RemoteAddr(), len(h.clients))
			h.mu.Unlock()

			// 优化：新客户端连接时，只发送最近的 10 条日志而非所有日志
			// 这样可以避免一次性发送大量数据导致前端卡顿
			if globalBuffer != nil {
				rawLogLines := globalBuffer.GetRawEntriesBytes()
				// 只发送最后 10 条日志
				startIdx := len(rawLogLines) - 10
				if startIdx < 0 {
					startIdx = 0
				}
				for i := startIdx; i < len(rawLogLines); i++ {
					err := client.WriteMessage(websocket.TextMessage, rawLogLines[i])
					if err != nil {
						log.Printf("错误: 发送历史日志到新客户端 %s 失败: %v", client.RemoteAddr(), err)
						// 如果发送失败，可能客户端已经断开，进行清理
						h.mu.Lock()
						delete(h.clients, client)
						client.Close()
						h.mu.Unlock()
						break // 停止向此客户端发送历史日志
					}
				}
			}

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.Close() // 确保关闭连接
				log.Printf("WebSocket 客户端已断开: %s. 当前客户端数量: %d", client.RemoteAddr(), len(h.clients))
			}
			h.mu.Unlock()

		case message := <-h.broadcast: // 这个分支通常不会再被使用，保留以防万一
			h.mu.Lock()
			for client := range h.clients {
				err := client.WriteMessage(websocket.TextMessage, message)
				if err != nil {
					log.Printf("错误: 广播日志到客户端 %s 失败: %v. 将其移除。", client.RemoteAddr(), err)
					delete(h.clients, client)
					client.Close()
				}
			}
			h.mu.Unlock()
		}
	}
}

// Broadcast 已弃用，不再使用。
// 之前频繁调用此方法导致大量系统调用和GDI泄漏。
// 现在日志只在客户端首次连接时发送历史日志。
// 如果需要实时日志推送，可以由前端定时拉取或实现其他机制。
func (h *Hub) Broadcast(message []byte) {
	// 空实现，不做任何事情
	// 这避免了之前每条日志都导致的系统调用
}

// RegisterClient 用于从外部（例如HTTP WebSocket处理器）注册客户端。
func (h *Hub) RegisterClient(client *websocket.Conn) {
	h.register <- client
}

// UnregisterClient 用于从外部（例如HTTP WebSocket处理器在连接关闭时）注销客户端。
func (h *Hub) UnregisterClient(client *websocket.Conn) {
	// 确保不会在已经关闭的channel上发送
	// 可以在Hub停止后检查channel是否关闭
	// 或者 Run 方法在退出前关闭这些channel（不推荐，因为它们是输入）
	// 更简单的是，如果Run已退出，这些操作会阻塞或panic，需要robust handling
	// 但在正常运行时，这是安全的。
	h.unregister <- client
}
