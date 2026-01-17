package ahk

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	ahkExecutableName = "AutoHotkey.exe"
)

// getAhkPath 获取程序目录下 ahk/ 文件夹中的 AutoHotkey.exe 路径
func getAhkPath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("获取程序路径失败: %w", err)
	}
	dir := filepath.Dir(exePath)
	ahkPath := filepath.Join(dir, "ahk", ahkExecutableName)
	return ahkPath, nil
}

// RunScriptAndGetProcess 执行指定的AHK脚本并返回 *os.Process 对象。
// scriptName 是脚本文件名，应与主程序在同一目录。
// args 是传递给脚本的参数。
func RunScriptAndGetProcess(scriptName string, args ...string) (*os.Process, error) {
	ahkPath, err := getAhkPath()
	if err != nil {
		return nil, err
	}

	scriptDir := filepath.Dir(os.Args[0])
	scriptFullPath := filepath.Join(scriptDir, "ahk", "script", scriptName)

	if _, err := os.Stat(scriptFullPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("脚本 %s 未找到于 %s", scriptName, scriptFullPath)
	}

	cmdArgs := append([]string{scriptFullPath}, args...)
	// 由上层逻辑负责管理脚本实例（启动/取消），此处直接启动新进程

	cmd := exec.Command(ahkPath, cmdArgs...)

	log.Printf("准备执行 AHK: %s %v", ahkPath, cmdArgs)
	err = cmd.Start() // 使用 Start 而不是 Run，以便获取 Process 对象
	if err != nil {
		return nil, fmt.Errorf("启动脚本 %s 失败: %w", scriptName, err)
	}
	log.Printf("脚本 %s 已异步启动 (PID: %d)。", scriptName, cmd.Process.Pid)

	// 注册到运行表，以便可以后续停止
	registerRunningScript(scriptFullPath, cmd.Process)
	return cmd.Process, nil
}

var (
	runningMu    sync.Mutex
	runningProcs = make(map[string]*os.Process) // key: full path
)

func registerRunningScript(fullPath string, p *os.Process) {
	runningMu.Lock()
	defer runningMu.Unlock()
	runningProcs[fullPath] = p
}

func unregisterRunningScript(fullPath string) {
	runningMu.Lock()
	defer runningMu.Unlock()
	delete(runningProcs, fullPath)
}

// IsScriptRunning returns true if a script with that name has been started by RunScriptAndGetProcess
func IsScriptRunning(scriptName string) bool {
	scriptDir := filepath.Dir(os.Args[0])
	scriptFullPath := filepath.Join(scriptDir, "ahk", "script", scriptName)
	runningMu.Lock()
	defer runningMu.Unlock()
	p, ok := runningProcs[scriptFullPath]
	if !ok || p == nil {
		return false
	}
	// try to signal 0 to check if process alive on Windows not trivial; assume if pid exists it's running
	return true
}

// StopScript kills the running script process launched earlier.
func StopScript(scriptName string) error {
	scriptDir := filepath.Dir(os.Args[0])
	scriptFullPath := filepath.Join(scriptDir, "ahk", "script", scriptName)
	runningMu.Lock()
	p, ok := runningProcs[scriptFullPath]
	runningMu.Unlock()
	if !ok || p == nil {
		return fmt.Errorf("no running script: %s", scriptName)
	}
	// attempt to kill
	if err := p.Kill(); err != nil {
		return err
	}
	unregisterRunningScript(scriptFullPath)
	return nil
}

// runAHKScriptViaStdin 将 AHK 脚本通过 stdin 传给 AutoHotkey.exe (/ErrorStdOut *) 并返回 stdout
func runAHKScriptViaStdin(ctx context.Context, script string) (string, error) {
	ahkPath, err := getAhkPath()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, ahkPath, "/ErrorStdOut", "*")
	cmd.Stdin = strings.NewReader(script)
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		// 尽量返回 stderr 以便排错
		combined := strings.TrimSpace(out.String() + "\n" + errOut.String())
		if combined == "" {
			return "", fmt.Errorf("AHK 执行失败: %w", err)
		}
		return combined, fmt.Errorf("AHK 执行失败: %w", err)
	}
	return strings.TrimSpace(out.String()), nil
}

// RunAhkCode 将给定的 AHK 脚本通过 stdin 传给 AutoHotkey 并返回 stdout 或错误。
func RunAhkCode(script string) (string, error) {
	return runAHKScriptViaStdin(context.Background(), script)
}

// GetScriptCountdownSeconds 从脚本文件中解析倒计时配置，支持多种命名（例如 countdownSeconds 或 DisplaySeconds）。
func GetScriptCountdownSeconds(scriptFullPath string) int {
	data, err := ioutil.ReadFile(scriptFullPath)
	if err != nil {
		return 5
	}
	content := string(data)
	lines := strings.Split(content, "\n")
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		// 支持多种写法，例如: countdownSeconds := 5 或 DisplaySeconds := 5 或 DisplaySeconds = 5
		if strings.Contains(ln, "countdownSeconds") || strings.Contains(ln, "DisplaySeconds") {
			// 从该行中提取最后一个整数
			parts := strings.FieldsFunc(ln, func(r rune) bool { return r == ':' || r == '=' || r == ' ' || r == '\t' || r == ',' })
			for i := len(parts) - 1; i >= 0; i-- {
				p := strings.Trim(parts[i], " \t\r\n")
				if n, err := strconv.Atoi(p); err == nil {
					return n
				}
			}
		}
	}
	return 5
}

// SetSystemVolumeAHK 使用 AHK 设置系统主音量（0-100）
func SetSystemVolumeAHK(ctx context.Context, v int) error {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	// AHK SoundSet 使用百分比
	script := fmt.Sprintf("SoundSet, %d, Master", v)
	_, err := runAHKScriptViaStdin(ctx, script)
	return err
}

// GetSystemVolumeAHK 使用 AHK 获取主音量（0-100）
func GetSystemVolumeAHK(ctx context.Context) (int, error) {
	script := "SoundGet, vol, Master\nFileAppend, %vol%, *"
	out, err := runAHKScriptViaStdin(ctx, script)
	if err != nil {
		return 50, err
	}
	out = strings.TrimSpace(out)
	// 有些系统可能返回带单位或额外文本，尝试提取数字
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return 50, fmt.Errorf("无法解析 AHK 输出: %q", out)
	}
	n, err := strconv.Atoi(strings.Trim(fields[0], "%"))
	if err != nil {
		return 50, err
	}
	if n < 0 {
		n = 0
	}
	if n > 100 {
		n = 100
	}
	return n, nil
}
