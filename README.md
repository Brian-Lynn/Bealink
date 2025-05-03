# Bealink Server 🚀       [中文](README_zh.md)
A lightweight background service program running on Windows, allowing remote control of your computer for sleep and shutdown operations, and clipboard content synchronization via LAN HTTP requests or Bonjour service discovery.

> ⚠️ Note: First project, has been running stably on 2 devices for 10 minutes. Welcome issues if you find bugs 😅
---
## ✨ Features
Use any device on the local network to:
- `/sleep`: Remote sleep, pops up an AHK window with a countdown and progress bar, can be cancelled by clicking anywhere.
- `/shutdown`: Remote shutdown, same logic as above.
- `/clip/<text>`: Remotely copy text to the local clipboard, supports URL decoding and shows a notification popup.
- `/ping`, `/`: Health check and welcome page.
- Bonjour/mDNS Service: Supports access via `http://<hostname>.local:8080` without needing the IP address.
- System Tray Icon: With menu options to set auto-start on boot, view logs, exit the program, etc.
---

## ⚙️ Tech Stack
- Main Language: Go
- Script Interaction: AutoHotkey v1
- Dependencies:
  - `net/http` - HTTP service
  - `os/exec` - Calling AHK scripts
  - `github.com/getlantern/systray` - Tray menu
  - `github.com/grandcat/zeroconf` - Bonjour service discovery
  - `github.com/atotto/clipboard` - Clipboard operations
  - `golang.org/x/sys/windows/registry` - Registry control for auto-start
---

## 🧪 Installation & Usage

### 1. Installation
Run the installer, it will:
- Copy the Go program and AHK scripts
- Install the AHK v1 interpreter (if not present)
- Automatically register the Bonjour service (will attempt silent install if not installed)
- Ask whether to start on boot & launch the program

### 2. Accessing the Service
Access via a browser or HTTP request tool from any device on the local network:
- `http://<YourComputerIP>:8080/sleep`
- `http://<YourComputerIP>:8080/shutdown`
- `http://<YourComputerIP>:8080/clip/Hello%20World`
If Bonjour is working correctly, you can also use:
- `http://<YourHostname>.local:8080`
---

## 🪪 Project Status

**Version: v1.0 Initially Usable**
Currently, the functionality is simple and intuitive. The main control paths and UI interaction logic have been implemented, but it hasn't been extensively tested in multi-device or multi-language environments. Issues and PRs are welcome ❤️

---

## 📄 License

MIT License.

---

## 🤝 Contributing & Feedback

If you find this little tool interesting, or if you encounter any issues while using it, please don't hesitate to provide feedback!
Pull Requests, Issues, suggestions, and even complaints are all welcome~

---

Made with ☕ and 🧠 by Brian
