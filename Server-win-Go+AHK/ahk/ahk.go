package ahk

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	// 需要 runtime 来判断操作系统以选择性地设置 SysProcAttr
)

const (
	ahkExecutableName = "AutoHotkey.exe"
)

var (
	globalAhkPath string
)

func FindAhkPath() (string, error) {
	if globalAhkPath != "" {
		return globalAhkPath, nil
	}
	exePath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exePath)
		localAhkPath := filepath.Join(dir, ahkExecutableName)
		if _, statErr := os.Stat(localAhkPath); statErr == nil {
			globalAhkPath = localAhkPath
			log.Printf("AHK路径设置为 (程序目录): %s", globalAhkPath)
			return globalAhkPath, nil
		}
	}
	path, err := exec.LookPath(ahkExecutableName)
	if err == nil {
		globalAhkPath = path
		log.Printf("AHK路径设置为 (系统PATH): %s", globalAhkPath)
		return globalAhkPath, nil
	}
	return "", fmt.Errorf("未在程序目录或系统PATH中找到 %s", ahkExecutableName)
}

// RunScriptAndGetProcess 执行指定的AHK脚本并返回 *os.Process 对象。
// scriptName 是脚本文件名，应与主程序在同一目录。
// args 是传递给脚本的参数。
func RunScriptAndGetProcess(scriptName string, args ...string) (*os.Process, error) {
	if globalAhkPath == "" {
		var findErr error
		_, findErr = FindAhkPath()
		if findErr != nil {
			return nil, findErr
		}
	}

	scriptDir := filepath.Dir(os.Args[0])
	scriptFullPath := filepath.Join(scriptDir, scriptName)

	if _, err := os.Stat(scriptFullPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("脚本 %s 未找到于 %s", scriptName, scriptFullPath)
	}

	cmdArgs := append([]string{scriptFullPath}, args...)
	cmd := exec.Command(globalAhkPath, cmdArgs...)

	// 在 Windows 上，如果希望 AHK 脚本窗口独立于 Go 程序的控制台，
	// 可以设置 SysProcAttr。但对于我们的目的（能够 Kill 进程），这通常不是必需的。
	// 如果 AHK 窗口不显示或行为异常，可以尝试取消下面这块的注释。
	/*
		if runtime.GOOS == "windows" {
			cmd.SysProcAttr = &syscall.SysProcAttr{
				HideWindow:    false, // 如果你想隐藏可能的AHK中间窗口（不推荐用于GUI脚本）
				CreationFlags: 0x00000008, // CREATE_NO_WINDOW (如果AHK脚本自身创建GUI，这个可能不需要)
											// 0x08000000, // DETACHED_PROCESS (使其完全独立)
			}
		}
	*/

	log.Printf("准备执行 AHK: %s %v", globalAhkPath, cmdArgs)
	err := cmd.Start() // 使用 Start 而不是 Run，以便获取 Process 对象
	if err != nil {
		return nil, fmt.Errorf("启动脚本 %s 失败: %w", scriptName, err)
	}
	log.Printf("脚本 %s 已异步启动 (PID: %d)。", scriptName, cmd.Process.Pid)

	// 不再在此处 go cmd.Wait()。调用者将负责管理进程生命周期或等待。
	return cmd.Process, nil
}
