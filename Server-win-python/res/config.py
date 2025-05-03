# -*- coding: utf-8 -*-

import socket

# --- 应用配置 ---
APP_NAME = "Bealink"
APP_VERSION = "1.4.1" # 版本号更新 (Bark 参数化)
DEBUG = False # 设置为 True 以在开发期间启用更详细的日志记录

# --- 服务器配置 ---
SERVER_PORT = 8080
SERVER_HOST = '0.0.0.0' # 监听所有可用的网络接口

# --- Bonjour/mDNS 配置 ---
SERVICE_TYPE = "_http._tcp.local."
try:
    HOSTNAME = socket.gethostname()
except Exception:
    HOSTNAME = "电脑" # 获取主机名失败时的备用名
INSTANCE_NAME = f"{HOSTNAME}._{APP_NAME}.{SERVICE_TYPE}"

# --- 系统操作配置 ---
ACTION_DELAY = 5 # 关机/睡眠操作的延迟时间（秒）

# --- Windows 特定配置 ---
STARTUP_REG_KEY = r"Software\Microsoft\Windows\CurrentVersion\Run" # 开机自启注册表路径

# --- 通知窗口配置 ---
NOTIFICATION_TITLE_FONT_FAMILY = "Microsoft YaHei"
NOTIFICATION_MESSAGE_FONT_FAMILY = "Microsoft YaHei"
NOTIFICATION_TITLE_FONT_SIZE = 12
NOTIFICATION_MESSAGE_FONT_SIZE = 10
NOTIFICATION_PADDING = 18
NOTIFICATION_CORNER_RADIUS_EFFECT = 2
NOTIFICATION_WRAPLENGTH = 380
NOTIFICATION_MAX_LINES = 6
NOTIFICATION_WIDTH_CHARS = 50
NOTIFICATION_MARGIN_X = 40
NOTIFICATION_MARGIN_Y = 70
NOTIFICATION_BG_COLOR = "#333333"
NOTIFICATION_FG_COLOR = "#FFFFFF"
NOTIFICATION_BORDER_COLOR = "#555555"
NOTIFICATION_ALPHA = 0.92
NOTIFICATION_TIMEOUT = 5000

# --- Bark 推送配置 ---
BARK_ENABLED = True # 设置为 True 以启用 Bark 推送

# **重要：请将 YOUR_BARK_KEY 替换为你自己的 Bark 设备密钥！**
# (必填项，如果 BARK_ENABLED = True)
BARK_DEVICE_KEY = "RHtD4CJXsQgka8vMjTxBYG"

# Bark API 服务器地址 (通常不需要修改)
BARK_API_SERVER = "https://api.day.app"

# 可选参数 (留空则使用 Bark 默认值或不指定)
BARK_GROUP_GENERAL = "Bealink"        # 通用消息分组名
BARK_GROUP_SYSTEM = "Bealink系统事件" # 系统事件分组名 (例如睡眠/唤醒)
BARK_ICON_URL = "https://day.app/assets/images/avatar.jpg"                   # 默认图标 URL (例如 "https://day.app/assets/images/avatar.jpg")
BARK_DEFAULT_SOUND = "glass"         # 默认推送声音
BARK_TIMEOUT = 5                   # 发送 Bark 请求的超时时间（秒）

# 特定事件的声音和图标 (可选, 留空则使用默认)
BARK_SOUND_STARTUP = "hello"
BARK_SOUND_SLEEP = "alarm"
BARK_SOUND_WAKE = "chime"
BARK_SOUND_SHUTDOWN = "minuet"
BARK_SOUND_CLIPBOARD = "" # 剪贴板推送通常不需要声音
BARK_ICON_STARTUP = BARK_ICON_URL # 可以为不同事件设置不同图标
BARK_ICON_SLEEP = BARK_ICON_URL
BARK_ICON_WAKE = BARK_ICON_URL
BARK_ICON_SHUTDOWN = BARK_ICON_URL
BARK_ICON_CLIPBOARD = BARK_ICON_URL

# 加密设置 (如果需要加密推送)
BARK_ENCRYPTION_ENABLED = False # 设置为 True 以启用加密
# **重要：如果启用加密，必须提供 16 位的密钥和 IV**
BARK_ENCRYPTION_KEY = "4A9F2C7D1E5B8FTH" # 必须是 16 个 ASCII 字符
BARK_ENCRYPTION_IV = "3B8E5F1C9D2G7H4J"  # 必须是 16 个 ASCII 字符 (可以固定，或每次随机生成)

# --- 键盘监听配置 ---
KEYBOARD_CANCEL_ENABLED = True # 设置为 True 以尝试启用键盘取消功能 (需要管理员权限)

