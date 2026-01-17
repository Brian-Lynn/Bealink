// ************************************************************************
// ** 文件: bark/bark.go (优化合并通知逻辑, 简化测试)                     **
// ** 描述: 实现向 Bark 服务发送推送通知的功能。                             **
// ** 主要改动：                                                     **
// ** - NotifyEvent 不再区分 startup/wakeup，统一为 system_ready 事件。 **
// ** - SendNotification 中 system_ready 事件的消息内容统一。         **
// ** - SendNotification 中 test 事件使用固定的标题和内容。           **
// ************************************************************************
package bark

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// 定义固定的测试推送标题和内容
	fixedTestTitle = "Bealink Go 服务 - 测试推送"
	fixedTestBody  = "这是一条来自 Bealink Go 服务的连接测试通知。"
)

type BarkNotifier struct {
	lastNotifyTimes map[string]time.Time
	mu              sync.Mutex
	httpClient      *http.Client
}

type NotificationPayload struct {
	Title      string `json:"title,omitempty"`
	Body       string `json:"body,omitempty"`
	Group      string `json:"group,omitempty"`
	Icon       string `json:"icon,omitempty"`
	Sound      string `json:"sound,omitempty"`
	URL        string `json:"url,omitempty"`
	Copy       string `json:"copy,omitempty"`
	AutoCopy   string `json:"autoCopy,omitempty"`
	IsArchive  string `json:"isArchive,omitempty"`
	Ciphertext string `json:"ciphertext,omitempty"`
	Iv         string `json:"iv,omitempty"`
}

var globalNotifier *BarkNotifier
var notifierOnce sync.Once

func GetNotifier() *BarkNotifier {
	notifierOnce.Do(func() {
		globalNotifier = &BarkNotifier{
			lastNotifyTimes: make(map[string]time.Time),
			httpClient:      &http.Client{Timeout: 15 * time.Second},
		}
		log.Println("Bark 通知器已初始化。")
	})
	return globalNotifier
}

func pkcs7Pad(data []byte, blockSize int) ([]byte, error) {
	if blockSize <= 0 {
		return nil, fmt.Errorf("无效的块大小: %d", blockSize)
	}
	padding := blockSize - (len(data) % blockSize)
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padText...), nil
}

func encryptAESCBC(plaintext []byte, key []byte, iv []byte) (string, error) {
	if len(key) != 16 {
		return "", fmt.Errorf("AES 加密密钥长度必须为 16 字节")
	}
	if len(iv) != 16 {
		return "", fmt.Errorf("AES 加密初始向量 (IV) 长度必须为 16 字节")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("创建 AES 密码块失败: %w", err)
	}
	paddedPlaintext, err := pkcs7Pad(plaintext, aes.BlockSize)
	if err != nil {
		return "", fmt.Errorf("PKCS7 填充失败: %w", err)
	}
	ciphertext := make([]byte, len(paddedPlaintext))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, paddedPlaintext)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func IsBarkConfigSufficient(cfg *BarkConfig) (sufficient bool, targetURL string, useEncryption bool, keyBytes, ivBytes []byte, reason string) {
	if cfg.BarkFullURL == "" {
		return false, "", false, nil, nil, "BarkFullURL 未配置"
	}
	parsedURL, errParse := url.Parse(cfg.BarkFullURL)
	if errParse != nil {
		return false, "", false, nil, nil, fmt.Sprintf("BarkFullURL ('%s') 解析失败: %v", cfg.BarkFullURL, errParse)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return false, "", false, nil, nil, fmt.Sprintf("BarkFullURL ('%s') 缺少 scheme (http/https) 或 host", cfg.BarkFullURL)
	}
	targetURL = strings.TrimSuffix(parsedURL.String(), "/")
	useEncryption = false
	var keyBytesOut, ivBytesOut []byte
	var encReason string
	if cfg.EncryptionKey != "" && cfg.EncryptionIV != "" {
		if len(cfg.EncryptionKey) == 16 && len(cfg.EncryptionIV) == 16 {
			keyBytesOut = []byte(cfg.EncryptionKey)
			ivBytesOut = []byte(cfg.EncryptionIV)
			useEncryption = true
		} else {
			encReason = "加密密钥或IV长度不为16位ASCII字符"
		}
	} else if cfg.EncryptionKey != "" || cfg.EncryptionIV != "" {
		encReason = "加密密钥和IV必须同时配置才有效"
	}
	if encReason != "" {
		log.Printf("警告: %s (Key len: %d, IV len: %d)。将不使用加密。", encReason, len(cfg.EncryptionKey), len(cfg.EncryptionIV))
	}
	return true, targetURL, useEncryption, keyBytesOut, ivBytesOut, encReason
}

func (bn *BarkNotifier) SendNotification(eventType string, title, body string, customIcon, customSound, customGroup, customURLPath, customCopy string, autoCopy, isArchive bool) {
	cfg := GetConfig()
	sufficient, targetURL, useEncryption, keyBytes, ivBytes, insufficientReason := IsBarkConfigSufficient(cfg)
	if !sufficient {
		logMsg := fmt.Sprintf("Bark 配置不完整 (%s)，无法发送通知 (事件: %s)。", insufficientReason, eventType)
		if eventType == "test" {
			log.Printf("错误: %s", logMsg)
		} else if insufficientReason != "" {
			log.Printf("信息: %s", logMsg)
		}
		return
	}
	if insufficientReason != "" && useEncryption {
		log.Printf("警告: 事件 '%s' 的通知将尝试以非加密方式发送，因为: %s", eventType, insufficientReason)
		useEncryption = false
	}

	bn.mu.Lock()
	if eventType != "test" { // 测试通知不受频率限制
		lastTime, found := bn.lastNotifyTimes[eventType]
		if found && time.Since(lastTime) < 2*time.Second {
			log.Printf("Bark 通知 (事件: %s) 触发过于频繁，已跳过。", eventType)
			bn.mu.Unlock()
			return
		}
	}
	bn.lastNotifyTimes[eventType] = time.Now()
	bn.mu.Unlock()

	finalTitle, finalBody := title, body
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "未知设备"
	}

	switch eventType {
	case "system_ready":
		finalTitle = "Bealink 服务"                        // 系统就绪的标题
		finalBody = fmt.Sprintf("💻 主机 %s 已就绪", hostname) // 系统就绪的内容
	case "test":
		finalTitle = fixedTestTitle // 使用固定的测试标题
		finalBody = fixedTestBody   // 使用固定的测试内容
		// 如果启用加密，测试推送内容加提示
		if useEncryption {
			finalBody += "\n（本通知通过加密通道发送）"
		}
		// 如果有其他 eventType，可以在这里添加 case
	}

	payload := NotificationPayload{
		Title: finalTitle, Body: finalBody, Group: customGroup, Icon: customIcon, Sound: customSound,
		URL: customURLPath, Copy: customCopy,
	}
	if payload.Group == "" {
		payload.Group = cfg.Group
	} // 如果自定义为空，则使用配置中的默认值
	if payload.Icon == "" {
		payload.Icon = cfg.IconURL
	}
	if payload.Sound == "" {
		payload.Sound = cfg.Sound
	}
	if autoCopy {
		payload.AutoCopy = "1"
	}
	if isArchive {
		payload.IsArchive = "1"
	}

	go bn.trySendWithRetry(eventType, targetURL, payload, useEncryption, keyBytes, ivBytes, cfg)
}

func (bn *BarkNotifier) trySendWithRetry(eventType string, targetURL string, payload NotificationPayload, useEncryption bool, keyBytes, ivBytes []byte, cfg *BarkConfig) {
	var requestDataBytes []byte
	var err error
	contentType := "application/json; charset=utf-8"

	if useEncryption {
		if keyBytes == nil || ivBytes == nil || len(keyBytes) != 16 || len(ivBytes) != 16 {
			log.Printf("错误: trySendWithRetry 内部加密参数无效 (事件: %s)。将尝试非加密发送。", eventType)
			useEncryption = false
		} else {
			corePayloadForEncryption := NotificationPayload{
				Title: payload.Title, Body: payload.Body, Group: payload.Group, Sound: payload.Sound,
				URL: payload.URL, Copy: payload.Copy, AutoCopy: payload.AutoCopy, IsArchive: payload.IsArchive,
			}
			if payload.Icon != "" {
				corePayloadForEncryption.Icon = payload.Icon
			}
			corePayloadJSON, jsonErr := json.Marshal(corePayloadForEncryption)
			if jsonErr != nil {
				log.Printf("错误: 序列化核心载荷JSON失败(加密, 事件: %s): %v。放弃。", eventType, jsonErr)
				return
			}
			ciphertext, encErr := encryptAESCBC(corePayloadJSON, keyBytes, ivBytes)
			if encErr != nil {
				log.Printf("错误: AES加密失败(事件: %s): %v。将尝试非加密发送。", eventType, encErr)
				useEncryption = false
			} else {
				encryptedPayload := NotificationPayload{Ciphertext: ciphertext, Iv: cfg.EncryptionIV}
				requestDataBytes, err = json.Marshal(encryptedPayload)
				if err != nil {
					log.Printf("错误: 序列化加密载荷JSON失败(事件: %s): %v。将尝试非加密发送。", eventType, err)
					useEncryption = false
				}
				if useEncryption {
					log.Printf("信息: Bark载荷已加密(事件: %s)。URL: %s。发送的IV: '%s'", eventType, targetURL, cfg.EncryptionIV)
				}
			}
		}
	}
	if !useEncryption {
		requestDataBytes, err = json.Marshal(payload)
		if err != nil {
			log.Printf("错误: 序列化非加密载荷JSON失败(事件: %s): %v。放弃。", eventType, err)
			return
		}
		log.Printf("信息: Bark载荷未加密(事件: %s)。URL: %s", eventType, targetURL)
	}

	for i := 0; i < cfg.MaxRetries; i++ {
		log.Printf("尝试发送Bark通知(事件: %s, 标题: %s, 尝试 %d/%d)...", eventType, payload.Title, i+1, cfg.MaxRetries)
		req, _ := http.NewRequest("POST", targetURL, bytes.NewBuffer(requestDataBytes))
		req.Header.Set("Content-Type", contentType)
		resp, postErr := bn.httpClient.Do(req)
		if postErr != nil {
			log.Printf("错误: 发送Bark通知HTTP请求失败(事件: %s, 尝试 %d/%d): %v", eventType, i+1, cfg.MaxRetries, postErr)
			if i < cfg.MaxRetries-1 {
				time.Sleep(time.Duration(cfg.RetryDelaySec) * time.Second)
				continue
			}
			break
		}
		respBodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var barkResp struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			if json.Unmarshal(respBodyBytes, &barkResp) == nil && barkResp.Code == 200 {
				log.Printf("Bark通知发送成功(事件: %s, 标题: %s, Bark消息: %s)", eventType, payload.Title, barkResp.Message)
				return
			}
			log.Printf("Bark通知发送成功(HTTP 200), 但响应解析失败/非标准(事件: %s, 标题: %s)。响应: %s", eventType, payload.Title, string(respBodyBytes))
			return
		}
		log.Printf("错误: Bark服务器返回HTTP %d (事件: %s, 尝试 %d/%d)。响应: %s", resp.StatusCode, eventType, i+1, cfg.MaxRetries, string(respBodyBytes))
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			log.Printf("信息: Bark收到 %d, 通常表示配置问题, 不再重试。", resp.StatusCode)
			return
		}
		if i < cfg.MaxRetries-1 {
			time.Sleep(time.Duration(cfg.RetryDelaySec) * time.Second)
		}
	}
	log.Printf("错误: 最终放弃发送Bark通知(事件: %s, 标题: %s)在%d次尝试后。", eventType, payload.Title, cfg.MaxRetries)
}

// SendTestNotification 发送测试通知。
// 标题和内容将使用 bark.go 文件中定义的固定值。
func (bn *BarkNotifier) SendTestNotification() {
	log.Println("准备发送 Bark 测试通知 (使用固定内容)...")
	// 调用 SendNotification，eventType 为 "test"，标题和内容参数留空，
	// SendNotification 内部会根据 eventType "test" 使用固定的测试文案。
	bn.SendNotification("test", "", "", "", "", "", "", "", false, false)
}

func NotifyEvent(eventType string) {
	cfg := GetConfig()
	notifier := GetNotifier()
	sufficient, _, useEnc, _, _, reason := IsBarkConfigSufficient(cfg)
	if !sufficient {
		log.Printf("Bark 功能未配置或配置不完整 (%s), 事件 '%s' 的通知将不会发送。", reason, eventType)
		return
	}
	if reason != "" && useEnc {
		log.Printf("警告: 事件 '%s' 的通知将尝试以非加密方式发送，因为加密配置存在问题: %s", eventType, reason)
	}

	if eventType == "system_ready" {
		if !cfg.NotifyOnSystemReady {
			log.Printf("根据配置，系统就绪事件 (%s) 的 Bark 通知已禁用。", eventType)
			return
		}
		log.Printf("触发 Bark 系统就绪通知: %s", eventType)
		notifier.SendNotification(eventType, "", "", "", "", "", "", "", false, false)
	} else if eventType == "test" {
		log.Printf("触发 Bark 测试通知: %s", eventType)
		notifier.SendTestNotification() // 这个函数内部会处理好一切
	} else {
		log.Printf("警告: 未知的 Bark 通知事件类型: %s。不发送通知。", eventType)
	}
}
