package winapi

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// IsDarkMode 检测 Windows 系统是否使用深色主题
// 返回 true 表示深色模式，false 表示浅色模式
func IsDarkMode() (bool, error) {
	// Windows 10/11 的主题设置存储在注册表中
	// HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize
	// AppsUseLightTheme: 0 = 深色, 1 = 浅色
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		// 如果注册表键不存在（可能是旧版 Windows），默认返回浅色模式
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("打开注册表键失败 (检测主题): %w", err)
	}
	defer key.Close()

	value, _, err := key.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		// 如果值不存在，默认返回浅色模式
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("读取注册表值失败 (检测主题): %w", err)
	}

	// AppsUseLightTheme: 0 = 深色模式, 1 = 浅色模式
	// 所以返回 !value (取反)
	return value == 0, nil
}
