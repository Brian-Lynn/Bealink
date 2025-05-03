# Bealink Go Server 🚀  [中文说明 (Chinese Documentation)](README_zh.md)
一个运行在 Windows 上的轻量级后台服务程序，允许通过局域网 HTTP 请求或 Bonjour 服务发现，远程控制你的电脑执行睡眠、关机操作，并同步剪贴板内容。

> ⚠️ 注意：第一个项目，已在2台设备上稳定运行10分钟，有bug欢迎issue😅
---
## ✨ 功能简介
使用任意局域网内设备进行：
- `/sleep`：远程睡眠，弹出带倒计时和进度条的 AHK 窗口，任意点击可取消。
- `/shutdown`：远程关机，逻辑同上。
- `/clip/<text>`：远程复制文本到本机剪贴板，支持 URL 解码并弹窗提示。
- `/ping`、`/`：健康检测和欢迎页。
- Bonjour/mDNS 服务：支持通过 `http://<主机名>.local:8080` 无 IP 访问。
- 系统托盘图标：带菜单选项，可设置开机自启、查看日志、退出程序等。
---

## ⚙️ 技术栈
- 主语言：Go
- 脚本交互：AutoHotkey v1
- 依赖库：
  - `net/http` - HTTP 服务
  - `os/exec` - 调用 AHK 脚本
  - `github.com/getlantern/systray` - 托盘菜单
  - `github.com/grandcat/zeroconf` - Bonjour 服务发现
  - `github.com/atotto/clipboard` - 剪贴板操作
  - `golang.org/x/sys/windows/registry` - 注册表控制开机自启
---

## 🧪 安装 & 使用

### 1. 安装
运行安装程序，会：
- 复制 Go 程序与 AHK 脚本
- 安装 AHK v1 解释器（如果没有）
- 自动注册 Bonjour 服务（如未安装会尝试静默安装）
- 询问是否开机启动 & 启动程序

### 2. 访问服务
通过任意局域网内设备浏览器或 HTTP 请求工具访问：
- `http://<你的电脑IP>:8080/sleep`
- `http://<你的电脑IP>:8080/shutdown`
- `http://<你的电脑IP>:8080/clip/Hello%20World`
若 Bonjour 正常工作，也可用：
- `http://<你的主机名>.local:8080`
---

## 🪪 项目状态

**版本：v1.0 初步可用**  
目前功能简单直观，已实现主要控制路径和 UI 交互逻辑，但尚未在多设备和多语言环境中广泛测试。欢迎大家提交 Issues 或 PR ❤️

---

## 📄 License

MIT License.

---

## 🤝 贡献 & 反馈

如果你觉得这个小工具有点意思，或者用起来踩到坑了，请不要吝啬提出反馈！  
Pull Requests、Issues、建议、吐槽统统欢迎～  

---

Made with ☕ and 🧠 by Brian
