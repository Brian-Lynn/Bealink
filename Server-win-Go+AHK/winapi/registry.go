package winapi

import (
	"fmt"
	"log"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	registryRunPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	registryValueName = "BealinkGoServer" // 注册表自启动项名称
)

// IsAutoStartEnabled 检查程序是否已设置为开机自启。
func IsAutoStartEnabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, registryRunPath, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist { // 键本身不存在，说明肯定没有我们的项
			return false, nil
		}
		return false, fmt.Errorf("打开注册表键失败 (检查自启): %w", err)
	}
	defer key.Close()

	_, _, err = key.GetStringValue(registryValueName)
	if err == nil {
		return true, nil // 值存在，已启用
	}
	if err == registry.ErrNotExist { // 值不存在，未启用
		return false, nil
	}
	return false, fmt.Errorf("读取注册表值失败 (检查自启): %w", err) // 其他读取错误
}

// EnableAutoStart 将程序设置为开机自启。
func EnableAutoStart() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取程序可执行文件路径失败: %w", err)
	}

	// 路径需要用引号括起来，以处理路径中可能存在的空格
	quotedPath := `"` + exePath + `"`

	key, _, err := registry.CreateKey(registry.CURRENT_USER, registryRunPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("创建或打开注册表键失败 (启用自启): %w", err)
	}
	defer key.Close()

	err = key.SetStringValue(registryValueName, quotedPath)
	if err != nil {
		return fmt.Errorf("写入注册表值失败 (启用自启): %w", err)
	}
	// log.Println("开机自启已启用。") // 日志由调用方 (main) 处理
	return nil
}

// DisableAutoStart 取消程序的开机自启设置。
func DisableAutoStart() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, registryRunPath, registry.WRITE) // 需要写入权限来删除
	if err != nil {
		if err == registry.ErrNotExist {
			log.Println("信息: 注册表Run键不存在，无需禁用开机自启。")
			return nil // 键不存在，认为操作成功（已经是禁用状态）
		}
		return fmt.Errorf("打开注册表键失败 (禁用自启): %w", err)
	}
	defer key.Close()

	err = key.DeleteValue(registryValueName)
	if err != nil && err != registry.ErrNotExist { // 如果值不存在，也算成功
		return fmt.Errorf("删除注册表值失败 (禁用自启): %w", err)
	}
	// log.Println("开机自启已禁用。") // 日志由调用方 (main) 处理
	return nil
}
