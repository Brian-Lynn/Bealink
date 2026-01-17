// ************************************************************************
// ** 文件: config/config.go (优化合并通知, 简化测试)                     **
// ** 描述: 用于加载、保存和管理 Bark 推送服务的配置。                       **
// ** 主要改动：                                                     **
// ** - 将 NotifyOnStartup 和 NotifyOnWakeup 合并为 NotifyOnSystemReady。**
// ** - 移除 DefaultTestTitle 和 DefaultTestBody 字段。             **
// ************************************************************************
package bark

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

const (
	configFileName    = "bealink_config.json"
	MinRetryInterval  = 5
	defaultRetryDelay = 10
	defaultMaxRetries = 5
)

// BarkConfig 结构体定义了 Bark 推送所需的配置项
type BarkConfig struct {
	// 【重要】用户填写的完整 Bark 推送 URL，包含设备 Key。
	// 程序将根据此字段是否有效配置来判断 Bark 推送功能是否启用。
	// 示例: "https://api.day.app/YOUR_DEVICE_KEY/"
	BarkFullURL string `json:"bark_full_url"`

	// (可选) 推送消息的默认分组名称。
	Group string `json:"group"`

	// (可选) 推送消息的默认图标 URL。
	IconURL string `json:"icon_url"`

	// (可选) 推送消息的默认提示音名称。
	Sound string `json:"sound"`

	// (可选) AES 加密密钥，必须是 16 个 ASCII 字符。
	// 如果此字段和 encryption_iv 字段都有效配置，则会自动启用加密推送。
	EncryptionKey string `json:"encryption_key"`

	// (可选) AES 加密的初始向量 (IV)，必须是 16 个 ASCII 字符。
	EncryptionIV string `json:"encryption_iv"`

	// (可选) 推送失败后的重试间隔时间（单位：秒）。
	RetryDelaySec int `json:"retry_delay_sec"`

	// (可选) 推送失败后的最大重试次数。
	MaxRetries int `json:"max_retries"`

	// 是否在系统就绪时（包括程序启动和从睡眠唤醒）发送一条 Bark 通知。
	// 需要 BarkFullURL 有效配置才会实际发送。
	NotifyOnSystemReady bool `json:"notify_on_system_ready"`

	// DefaultTestTitle string `json:"default_test_title"` // -- 已移除
	// DefaultTestBody  string `json:"default_test_body"`  // -- 已移除

	mu sync.RWMutex `json:"-"`
}

var globalConfig *BarkConfig
var once sync.Once
var configFilePath string

func getConfigDir() string {
	// 直接使用用户配置目录，不尝试程序目录
	appDataDir, err := os.UserConfigDir()
	if err != nil {
		log.Printf("警告: 获取用户配置目录失败，将使用当前工作目录: %v", err)
		return "."
	}
	configDir := filepath.Join(appDataDir, "BeaLink")
	log.Printf("信息: 配置文件将保存在用户配置目录: %s", configDir)
	return configDir
}

func createDefaultConfig() *BarkConfig {
	return &BarkConfig{
		BarkFullURL: "", Group: "", IconURL: "", Sound: "",
		EncryptionKey: "", EncryptionIV: "",
		RetryDelaySec: defaultRetryDelay, MaxRetries: defaultMaxRetries,
		NotifyOnSystemReady: true, // 默认启用系统就绪通知
		// DefaultTestTitle: "Bealink 测试通知", // -- 已移除
		// DefaultTestBody: "这是一条来自 Bealink Go 服务的测试推送。", // -- 已移除
	}
}

func InitConfig() {
	once.Do(func() {
		configDir := getConfigDir()
		configFilePath = filepath.Join(configDir, configFileName)
		log.Printf("配置文件完整路径设置为: %s", configFilePath)
		globalConfig = createDefaultConfig()
		log.Printf("调试: InitConfig - 已设置初始默认值到 globalConfig。内存中 BarkFullURL: '%s'", globalConfig.BarkFullURL)
		err := LoadConfig()
		if err != nil {
			if os.IsNotExist(err) {
				log.Printf("提示: 配置文件 (%s) 不存在。将使用内存中的默认配置并创建新文件。", configFilePath)
				if errSave := saveConfigInternal(); errSave != nil {
					log.Printf("!!! 严重错误: 保存初始默认配置文件 %s 失败: %v", configFilePath, errSave)
				} else {
					log.Printf("信息: 已在 %s 创建包含默认值的配置文件。", configFilePath)
				}
			} else {
				log.Printf("!!! 错误: 加载配置文件 %s 失败: %v。程序将使用硬编码的默认值。", configFilePath, err)
			}
		} else {
			log.Printf("信息: 成功从 %s 加载配置并已更新到内存。内存中 BarkFullURL: '%s'", configFilePath, globalConfig.BarkFullURL)
		}
		log.Printf("调试: InitConfig 完成。最终内存中 BarkFullURL: '%s', NotifyOnSystemReady: %t", globalConfig.BarkFullURL, globalConfig.NotifyOnSystemReady)
	})
}

func GetConfig() *BarkConfig {
	if globalConfig == nil {
		InitConfig()
	}
	globalConfig.mu.RLock()
	defer globalConfig.mu.RUnlock()
	// 不直接复制包含互斥锁的整个结构，逐字段复制以避免拷贝锁值
	cfg := &BarkConfig{
		BarkFullURL:         globalConfig.BarkFullURL,
		Group:               globalConfig.Group,
		IconURL:             globalConfig.IconURL,
		Sound:               globalConfig.Sound,
		EncryptionKey:       globalConfig.EncryptionKey,
		EncryptionIV:        globalConfig.EncryptionIV,
		RetryDelaySec:       globalConfig.RetryDelaySec,
		MaxRetries:          globalConfig.MaxRetries,
		NotifyOnSystemReady: globalConfig.NotifyOnSystemReady,
	}
	return cfg
}

func UpdateConfig(updateFn func(cfgToUpdate *BarkConfig)) error {
	if globalConfig == nil {
		InitConfig()
	}
	globalConfig.mu.Lock()
	defer globalConfig.mu.Unlock()
	log.Printf("调试: UpdateConfig - 更新前内存中的 BarkFullURL: '%s', NotifyOnSystemReady: %t", globalConfig.BarkFullURL, globalConfig.NotifyOnSystemReady)
	updateFn(globalConfig)
	log.Printf("调试: UpdateConfig - 更新后内存中的 BarkFullURL: '%s', NotifyOnSystemReady: %t", globalConfig.BarkFullURL, globalConfig.NotifyOnSystemReady)
	err := saveConfigInternal()
	if err == nil {
		log.Printf("调试: UpdateConfig - 配置已成功保存到文件。")
	}
	return err
}

func LoadConfig() error {
	if globalConfig == nil {
		return fmt.Errorf("严重内部错误: LoadConfig 被调用时 globalConfig 尚未被 InitConfig 初始化")
	}
	log.Printf("调试: LoadConfig 开始，尝试读取文件: %s", configFilePath)
	data, err := os.ReadFile(configFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("调试: LoadConfig - 配置文件 %s 不存在。", configFilePath)
			return err
		}
		return fmt.Errorf("读取配置文件 %s 失败: %w", configFilePath, err)
	}
	log.Printf("调试: LoadConfig - 成功读取文件 %s, 内容长度: %d", configFilePath, len(data))

	if err := json.Unmarshal(data, globalConfig); err != nil {
		log.Printf("!!! 警告: 解析配置文件 %s (JSON) 失败: %v。globalConfig 可能未被文件内容更新。", configFilePath, err)
		return err
	}
	log.Printf("调试: LoadConfig - JSON Unmarshal 完成。内存中 BarkFullURL: '%s', NotifyOnSystemReady: %t", globalConfig.BarkFullURL, globalConfig.NotifyOnSystemReady)

	if globalConfig.RetryDelaySec < MinRetryInterval {
		log.Printf("警告: 从配置文件加载的 RetryDelaySec (%d) 小于最小值 (%d)，已修正为最小值。", globalConfig.RetryDelaySec, MinRetryInterval)
		globalConfig.RetryDelaySec = MinRetryInterval
	}
	if globalConfig.MaxRetries <= 0 {
		log.Printf("警告: 从配置文件加载的 MaxRetries (%d) 无效，已修正为默认值 %d。", globalConfig.MaxRetries, defaultMaxRetries)
		globalConfig.MaxRetries = defaultMaxRetries
	}
	return nil
}

func saveConfigInternal() error {
	log.Printf("调试: saveConfigInternal 开始，准备序列化内存中的 BarkFullURL: '%s', NotifyOnSystemReady: %t", globalConfig.BarkFullURL, globalConfig.NotifyOnSystemReady)
	data, err := json.MarshalIndent(globalConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置到 JSON 失败: %w", err)
	}
	dir := filepath.Dir(configFilePath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if mkErr := os.MkdirAll(dir, 0750); mkErr != nil {
			return fmt.Errorf("创建配置目录 %s 失败: %w", dir, mkErr)
		}
	}
	if err := os.WriteFile(configFilePath, data, 0640); err != nil {
		return fmt.Errorf("写入配置文件 %s 失败: %w", configFilePath, err)
	}
	log.Printf("信息: 配置已成功保存到 %s。确认文件中 BarkFullURL 应为: '%s', NotifyOnSystemReady: %t", configFilePath, globalConfig.BarkFullURL, globalConfig.NotifyOnSystemReady)
	return nil
}

func SaveConfig() error {
	if globalConfig == nil {
		InitConfig()
	}
	globalConfig.mu.Lock()
	defer globalConfig.mu.Unlock()
	return saveConfigInternal()
}

func GetConfigFilePath() string {
	if configFilePath == "" {
		InitConfig()
	}
	return configFilePath
}

func OpenConfigFile() error {
	filePath := GetConfigFilePath()
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Printf("配置文件 %s 不存在，尝试创建默认配置...", filePath)
		if errCreate := SaveConfig(); errCreate != nil {
			return fmt.Errorf("尝试创建默认配置文件 %s 失败: %w", filePath, errCreate)
		}
		log.Printf("默认配置文件已创建于: %s。请再次尝试打开。", filePath)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", filePath)
	case "darwin":
		cmd = exec.Command("open", filePath)
	case "linux":
		cmd = exec.Command("xdg-open", filePath)
	default:
		return fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}
	log.Printf("尝试使用系统默认程序打开配置文件: %s", filePath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动命令打开配置文件 %s 失败: %w", filePath, err)
	}
	return nil
}
