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
	broadcast  chan []byte              // 从日志系统传入的消息
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

			// 新客户端连接时，发送所有历史日志
			// 注意：这可能会发送大量数据，对于非常大的缓冲区可能需要优化
			// 或者前端只请求最近的N条，然后开始实时接收
			if globalBuffer != nil {
				// 发送历史日志时，最好逐条发送，避免单条消息过大
				// 并且前端需要能处理这种初始批量数据
				rawLogLines := globalBuffer.GetRawEntriesBytes()
				for _, lineBytes := range rawLogLines {
					err := client.WriteMessage(websocket.TextMessage, lineBytes)
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

		case message := <-h.broadcast: // 从 RingBuffer 的 Write 方法接收到广播请求
			h.mu.Lock()
			for client := range h.clients {
				err := client.WriteMessage(websocket.TextMessage, message)
				if err != nil {
					log.Printf("错误: 广播日志到客户端 %s 失败: %v. 将其移除。", client.RemoteAddr(), err)
					// 如果写入失败，说明客户端可能已断开，从map中移除并关闭连接
					// 需要在 goroutine 中处理，避免阻塞广播循环
					// 或者直接在这里处理，但要注意 unregister channel 的使用
					// 为简单起见，直接删除并关闭
					delete(h.clients, client)
					client.Close() // 确保关闭
				}
			}
			h.mu.Unlock()
		}
	}
}

// Broadcast 将消息发送到广播通道，由 Hub 的 Run 方法处理。
// 这个方法被 RingBuffer 的 Write 方法调用。
func (h *Hub) Broadcast(message []byte) {
	// 使用非阻塞发送，以防 Run 循环处理不及时导致日志写入阻塞
	// 但如果 broadcast channel 满了，日志会丢失。
	// 对于日志广播，通常我们不希望它阻塞日志记录本身。
	// 或者，可以稍微增大 broadcast channel 的缓冲区。
	select {
	case h.broadcast <- message:
	default:
		// log.Println("警告: WebSocket广播通道已满，部分日志可能未实时推送。")
		// 这个警告可能会产生大量日志，所以通常会注释掉或用更智能的方式处理
	}
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
