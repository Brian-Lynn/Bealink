package server

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ==========================================
// 1. 音量控制 (使用 AHK - 极速、稳定)
// ==========================================

// AHK 脚本：获取音量
const ahkGetVolumeScript = `
SoundGet, vol, Master
FileAppend, %vol%, *
`

// AHK 脚本：设置音量
const ahkSetVolumeScript = `
volume := %s
SoundSet, %%volume%%, Master
`

// AHK 脚本：静音切换
const ahkToggleMuteScript = `
SoundSet, +1, Master, Mute
`

// GetVolume 调用本地 AutoHotkey.exe 获取当前系统音量
func GetVolume() int {
	// 使用 AHK 实现获取音量
	ahk := getAhkPath()
	cmd := exec.Command(ahk, "/ErrorStdOut", "*")
	cmd.Stdin = strings.NewReader(ahkGetVolumeScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err := cmd.Output()
	if err != nil {
		log.Printf("[System] 获取音量失败 (请确认 AutoHotkey.exe 存在): %v", err)
		return 0
	}

	// AHK 输出通常是 "25.000000"，解析为 float 后四舍五入转 int
	volStr := strings.TrimSpace(string(output))
	val, err := strconv.ParseFloat(volStr, 64)
	if err != nil {
		log.Printf("[System] 解析音量值失败: %v (原始值: %q)", err, volStr)
		return 0
	}
	// 四舍五入到最接近的整数
	return int(val + 0.5)
}

// SetVolume 调用本地 AutoHotkey.exe 设置音量
func SetVolume(vol int) {
	if vol < 0 {
		vol = 0
	}
	if vol > 100 {
		vol = 100
	}
	script := fmt.Sprintf(ahkSetVolumeScript, strconv.Itoa(vol))
	ahk := getAhkPath()
	cmd := exec.Command(ahk, "/ErrorStdOut", "*")
	cmd.Stdin = strings.NewReader(script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	err := cmd.Run()
	if err != nil {
		log.Printf("[System] 设置音量失败: %v", err)
	}
}

// ToggleMute 切换静音状态
func ToggleMute() {
	ahk := getAhkPath()
	cmd := exec.Command(ahk, "/ErrorStdOut", "*")
	cmd.Stdin = strings.NewReader(ahkToggleMuteScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Run(); err != nil {
		log.Printf("[System] 静音切换失败: %v", err)
	} else {
		log.Println("[System] 静音状态已切换")
	}
}

// ==========================================
// 2. 媒体信息
// ==========================================

type MediaInfo struct {
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Status   string `json:"status"` // Playing, Paused, Stopped
	HasMedia bool   `json:"hasMedia"`
}

// GetMediaInfo 返回简化的固定媒体控件占位
func GetMediaInfo() MediaInfo {
	return MediaInfo{Title: "媒体控制", Artist: "", Status: "Stopped", HasMedia: false}
}

// getAhkPath 获取程序目录下 ahk/ 文件夹中的 AutoHotkey.exe 路径
func getAhkPath() string {
	exePath, err := os.Executable()
	if err != nil {
		return "AutoHotkey.exe" // 回退到默认名称
	}
	dir := filepath.Dir(exePath)
	return filepath.Join(dir, "ahk", "AutoHotkey.exe")
}
