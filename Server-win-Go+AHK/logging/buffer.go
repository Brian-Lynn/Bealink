package logging

import (
	"bytes"
	"io"
	"log"
	"sync"
	"time"
)

// LogEntry 代表一条日志记录
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	Raw       string    `json:"-"` // 原始格式化后的日志行，用于直接输出
}

// RingBuffer 是一个线程安全的环形日志缓冲区
type RingBuffer struct {
	mu      sync.Mutex
	entries []LogEntry
	maxSize int
	head    int  // 指向下一个写入位置
	full    bool // 缓冲区是否已满（即已发生覆盖）
}

// NewRingBuffer 创建一个新的环形缓冲区
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = 100 // 默认大小
	}
	return &RingBuffer{
		entries: make([]LogEntry, size),
		maxSize: size,
	}
}

// Write 实现 io.Writer 接口，将日志写入缓冲区
// 日志只存储在内存中的环形缓冲区，并通过 WebSocket hub 广播到 HTML 界面
func (rb *RingBuffer) Write(p []byte) (n int, err error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// 创建 LogEntry 并存入环形缓冲区（不写入任何文件，只通过 WebSocket 显示）
	msg := string(bytes.TrimSpace(p)) // 去除末尾的换行符
	entry := LogEntry{
		Timestamp: time.Now(),
		Message:   msg, // Message 可以考虑只取 p 中核心部分，如果 p 包含过多 log 包的前缀
		Raw:       msg, // Raw 存储完整的格式化行
	}

	rb.entries[rb.head] = entry
	rb.head = (rb.head + 1) % rb.maxSize
	if !rb.full && rb.head == 0 { // 首次写满
		rb.full = true
	}

	// 3. 通过 WebSocket hub 广播 (如果 hub 存在)
	if globalHub != nil {
		// globalHub.Broadcast(entry) // 直接广播原始字节 p 可能更高效
		globalHub.Broadcast(p) // 广播原始字节，前端可以决定如何解析或直接显示
	}

	return len(p), nil
}

// GetEntries 返回缓冲区中的所有日志条目，按时间顺序（旧 -> 新）
func (rb *RingBuffer) GetEntries() []LogEntry {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	var result []LogEntry
	if !rb.full { // 缓冲区未满
		result = make([]LogEntry, rb.head)
		copy(result, rb.entries[:rb.head])
	} else { // 缓冲区已满，需要从 head 开始环绕读取
		result = make([]LogEntry, rb.maxSize)
		copy(result, rb.entries[rb.head:])
		copy(result[rb.maxSize-rb.head:], rb.entries[:rb.head])
	}
	return result
}

// GetRawEntriesBytes 返回缓冲区中所有日志的原始字节表示，用于简单文本显示
func (rb *RingBuffer) GetRawEntriesBytes() [][]byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	var resultBytes [][]byte
	if !rb.full {
		for i := 0; i < rb.head; i++ {
			resultBytes = append(resultBytes, []byte(rb.entries[i].Raw))
		}
	} else {
		for i := 0; i < rb.maxSize; i++ {
			idx := (rb.head + i) % rb.maxSize
			resultBytes = append(resultBytes, []byte(rb.entries[idx].Raw))
		}
	}
	return resultBytes
}

var (
	globalBuffer *RingBuffer
	globalHub    *Hub // 声明，将在 hub.go 中定义和初始化
)

// Init 初始化全局日志缓冲区和 WebSocket hub，并返回一个 io.Writer
// 此函数应在程序早期调用，例如 main 函数的开头。
func Init(bufferSize int) io.Writer {
	globalBuffer = NewRingBuffer(bufferSize)
	globalHub = NewHub() // 初始化 Hub
	log.Printf("日志系统初始化完成。缓冲区大小: %d", bufferSize)
	return globalBuffer // log.SetOutput 将使用这个 writer
}

// GetBuffer 返回全局日志缓冲区实例
func GetBuffer() *RingBuffer {
	return globalBuffer
}

// GetHub 返回全局 WebSocket Hub 实例
func GetHub() *Hub {
	if globalHub == nil {
		// 这是一个备用初始化，以防 Init 没有被正确调用或顺序问题
		// 但理想情况下，Init 应该确保 globalHub 被创建
		log.Println("警告: logging.GetHub() 被调用时 globalHub 为 nil，尝试重新初始化。")
		globalHub = NewHub()
	}
	return globalHub
}
