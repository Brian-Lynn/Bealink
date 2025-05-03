# -*- coding: utf-8 -*-

import os
import sys
import threading
import logging
import tkinter as tk
# import tkinter.ttk as ttk # 移除未使用的导入
import queue
from flask import Flask, request, jsonify, render_template_string
from PIL import Image, ImageDraw
import pystray
import pyperclip
import winreg
import ctypes
from zeroconf import ServiceInfo, Zeroconf, IPVersion
import socket
import time
# 导入用于查找和控制窗口的库
try:
    import win32gui
    import win32con
    get_console_window = ctypes.windll.kernel32.GetConsoleWindow
    CAN_CONTROL_CONSOLE = True
except (ImportError, AttributeError):
    print("警告: 未找到 pywin32 库或 GetConsoleWindow 函数。无法实现显示/隐藏控制台窗口功能。")
    logging.warning("未找到 pywin32 库或 GetConsoleWindow 函数。无法实现显示/隐藏控制台窗口功能。")
    CAN_CONTROL_CONSOLE = False
    get_console_window = lambda: 0 # type: ignore

# --- 配置 ---
APP_NAME = "Bealink"
APP_VERSION = "1.1" # 版本号更新
SERVER_PORT = 8080
SERVICE_TYPE = "_http._tcp.local." # 服务类型
# 服务名，通常是<实例名>.<服务类型>.<域>
# 这里使用 主机名._应用名.<服务类型>
INSTANCE_NAME = f"{socket.gethostname()}._{APP_NAME}.{SERVICE_TYPE}"
STARTUP_REG_KEY = r"Software\Microsoft\Windows\CurrentVersion\Run"
ACTION_DELAY = 5 # seconds

# --- 全局变量 ---
app = Flask(__name__)
tray_icon = None
zeroconf_instance: Zeroconf | None = None # 重命名以区分实例和模块
service_info: ServiceInfo | None = None
server_thread: threading.Thread | None = None
stop_event = threading.Event()
console_hwnd = 0
pending_action_timer: threading.Timer | None = None
cancel_action_event = threading.Event()
notification_queue: queue.Queue = queue.Queue()
root_tk: tk.Tk | None = None
gui_thread: threading.Thread | None = None
queue_processor_thread: threading.Thread | None = None # 声明以便全局访问

# --- 工具函数 ---

def setup_logging():
    """配置日志记录到控制台"""
    log_formatter = logging.Formatter('%(asctime)s - %(levelname)s - %(threadName)s - %(message)s')
    root_logger = logging.getLogger()
    root_logger.setLevel(logging.INFO)
    # 移除可能存在的默认处理器，避免重复日志
    if root_logger.hasHandlers():
        root_logger.handlers.clear()
    console_handler = logging.StreamHandler(sys.stdout)
    console_handler.setFormatter(log_formatter)
    root_logger.addHandler(console_handler)
    logging.info(f"--- {APP_NAME} v{APP_VERSION} 已启动 ---")
    logging.info("日志将输出到此控制台窗口。")

def get_local_ip():
    """获取本机的局域网 IP 地址 (改进版)"""
    possible_ips = []
    # 方法一：连接外部地址获取默认路由接口IP
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as s:
            s.settimeout(0.1)
            s.connect(('8.8.8.8', 80)) # 连接 Google DNS
            ip = s.getsockname()[0]
            if ip and ip != '0.0.0.0': # 确保获取到有效 IP
                possible_ips.append(ip)
            logging.debug(f"方法一 (连接外部) 获取 IP: {ip}")
    except Exception as e:
        logging.debug(f"方法一获取 IP 失败: {e}")

    # 方法二：获取所有接口地址
    try:
        hostname = socket.gethostname()
        # 获取与主机名关联的所有 IPv4 地址
        addr_info = socket.getaddrinfo(hostname, None, socket.AF_INET)
        for item in addr_info:
            ip = item[4][0]
            if ip and ip not in possible_ips:
                possible_ips.append(ip)
        logging.debug(f"方法二 (getaddrinfo) 获取 IPs: {possible_ips}")
    except socket.gaierror:
         logging.warning(f"无法解析主机名 '{hostname}' 获取 IP 地址。")
    except Exception as e:
        logging.debug(f"方法二获取 IP 失败: {e}")

    # 筛选最佳 IP
    best_ip = '127.0.0.1' # 默认回环地址
    # 优先选择常见的私有网段
    private_prefixes = ('192.168.', '10.', '172.16.', '172.17.', '172.18.', '172.19.',
                        '172.20.', '172.21.', '172.22.', '172.23.', '172.24.', '172.25.',
                        '172.26.', '172.27.', '172.28.', '172.29.', '172.30.', '172.31.')
    for ip in possible_ips:
        # 排除常见的虚拟网络/Docker网段 (如 172.17.*)，但保留大部分 172.*
        # 198.18.0.1 是用于网络基准测试的保留地址段，明确排除
        if ip.startswith(private_prefixes) and not ip.startswith('198.18.'):
             best_ip = ip
             logging.debug(f"优先选择私有 IP: {best_ip}")
             break # 找到一个就用

    # 如果没有找到理想的私有 IP，则选择第一个非回环、非链接本地(169.254)的 IP
    if best_ip == '127.0.0.1':
        for ip in possible_ips:
            if not ip.startswith('127.') and not ip.startswith('169.254.') and not ip.startswith('198.18.'):
                best_ip = ip
                logging.debug(f"选择第一个非特殊 IP: {best_ip}")
                break

    logging.info(f"最终选择的本机 IP: {best_ip}")
    return best_ip


def is_admin():
    """检查脚本是否以管理员权限运行"""
    try:
        return ctypes.windll.shell32.IsUserAnAdmin() != 0
    except AttributeError:
        logging.error("无法检查管理员权限，ctypes 或 shell32 不可用。假定为非管理员。")
        return False
    except Exception as e:
        logging.error(f"检查管理员权限时出错: {e}")
        return False

def run_command(command):
    """执行系统命令"""
    logging.info(f"执行命令: {command}")
    try:
        # 使用 subprocess 可以更好地控制和获取输出，但 os.system 对于简单命令更直接
        ret_code = os.system(command)
        if ret_code != 0:
            logging.error(f"命令 '{command}' 执行失败，返回码: {ret_code}")
            return False
        return True
    except Exception as e:
        logging.error(f"执行命令 '{command}' 时发生异常: {e}", exc_info=True)
        # 确保 show_custom_notification 在这里被调用是线程安全的
        # 它会将任务放入队列，由 GUI 线程处理
        show_custom_notification(f"{APP_NAME} 错误", f"执行命令失败: {command}\n{e}")
        return False

# --- 自定义通知窗口 (Tkinter) ---

def _create_notification_window(title, message, is_cancellable=False, action_type=None):
    """在 GUI 线程中创建和显示 Tkinter 通知窗口"""
    global root_tk # 确保访问的是全局变量
    if not root_tk:
        logging.error("_create_notification_window: GUI 根窗口尚未创建或已被销毁。")
        return

    try:
        window = tk.Toplevel(root_tk)
        window.withdraw() # 先隐藏
        window.overrideredirect(True) # 无边框窗口
        window.attributes('-topmost', True) # 置顶
        window.attributes('-alpha', 0.92) # 轻微透明

        # --- 外观参数调整 ---
        bg_color = "#333333" # 背景色
        fg_color = "#FFFFFF" # 前景色
        border_color = "#555555" # 边框色
        # 字体设置: 第一个参数是字体名称，第二个是大小，第三个是样式 (可选 "bold", "italic")
        # 你可以改成 "Microsoft YaHei" (微软雅黑) 或其他系统支持的字体
        font_title_family = "Segoe UI" # <--- 在这里修改标题字体名称
        font_message_family = "Segoe UI" # <--- 在这里修改内容字体名称
        font_title_size = 12 # <--- 在这里修改标题字体大小
        font_message_size = 10 # <--- 在这里修改内容字体大小
        font_title = (font_title_family, font_title_size, "bold")
        font_message = (font_message_family, font_message_size)
        padding = 18 # <--- 在这里修改内边距 (影响整体大小)
        corner_radius_effect = 2 # <--- 边框厚度模拟圆角效果 (0 表示无边框)
        text_wraplength = 380 # <--- 在这里修改文本换行宽度 (影响窗口宽度)
        text_max_lines = 6 # <--- 在这里修改长文本显示的最大行数
        text_width_chars = 50 # <--- 在这里修改长文本框的字符宽度
        # --- 外观参数调整结束 ---

        # 使用 Frame 作为容器，方便设置背景和内边距
        frame = tk.Frame(window, bg=bg_color, padx=padding, pady=padding,
                         highlightthickness=corner_radius_effect, # 用边框厚度模拟圆角
                         highlightbackground=border_color, # 边框颜色
                         highlightcolor=border_color) # 聚焦时边框颜色 (虽然窗口通常不聚焦)
        frame.pack(fill=tk.BOTH, expand=True)

        lbl_title = tk.Label(frame, text=title, bg=bg_color, fg=fg_color, font=font_title,
                             wraplength=text_wraplength, justify=tk.LEFT, anchor='w') # 使用调整后的宽度
        lbl_title.pack(fill=tk.X, pady=(0, 8)) # 标题和内容间距

        # 根据内容长度决定使用 Label 还是带滚动条的 Text
        if action_type == 'clipboard' and len(message) > 150: # 使用阈值判断是否为长文本
             # 使用 Text 组件显示长文本
             text_message = tk.Text(frame, wrap=tk.WORD, height=text_max_lines, width=text_width_chars, # 使用调整后的参数
                                   bg="#444444", fg=fg_color, font=font_message, relief=tk.FLAT,
                                   padx=5, pady=5, highlightthickness=0, borderwidth=0)
             text_message.insert(tk.END, message)
             text_message.config(state=tk.DISABLED) # 禁止编辑

             # 创建滚动条
             scrollbar = tk.Scrollbar(frame, command=text_message.yview, bg=bg_color,
                                      troughcolor="#555", activerelief=tk.FLAT, relief=tk.FLAT, width=12) # 稍微加宽滚动条
             text_message['yscrollcommand'] = scrollbar.set

             # 布局 Text 和 Scrollbar
             scrollbar.pack(side=tk.RIGHT, fill=tk.Y, padx=(5,0))
             text_message.pack(side=tk.LEFT, fill=tk.BOTH, expand=True)
        else:
             # 使用 Label 显示短文本
             lbl_message = tk.Label(frame, text=message, bg=bg_color, fg=fg_color, font=font_message,
                                    wraplength=text_wraplength, justify=tk.LEFT, anchor='nw') # 左上对齐
             lbl_message.pack(fill=tk.X)

        # 取消按钮逻辑
        if is_cancellable:
            def _cancel_action(event=None):
                logging.info(f"用户点击取消 {action_type} 操作。")
                cancel_action_event.set() # 设置取消标志
                safe_destroy(window) # 安全地关闭通知窗口
                # 在主线程中安排显示取消通知 (使用 after 确保在 GUI 线程执行)
                if root_tk:
                    root_tk.after(10, show_custom_notification, f"{APP_NAME}", f"{action_type.capitalize()} 操作已取消。", timeout=3)

            # 让整个窗口可点击取消
            frame.bind("<Button-1>", _cancel_action)
            lbl_title.bind("<Button-1>", _cancel_action)
            if 'lbl_message' in locals(): lbl_message.bind("<Button-1>", _cancel_action)
            if 'text_message' in locals(): text_message.bind("<Button-1>", _cancel_action)
            # 添加视觉提示
            frame.config(cursor="hand2")
            lbl_title.config(cursor="hand2")
            if 'lbl_message' in locals(): lbl_message.config(cursor="hand2")
            if 'text_message' in locals(): text_message.config(cursor="hand2")


        # 计算窗口位置并显示
        window.update_idletasks() # 更新尺寸信息
        screen_width = window.winfo_screenwidth()
        screen_height = window.winfo_screenheight()
        width = window.winfo_width()
        height = window.winfo_height()
        # --- 位置参数调整 ---
        margin_x = 40 # <--- 离屏幕右边的距离
        margin_y = 70 # <--- 离屏幕底边的距离 (考虑任务栏)
        # --- 位置参数调整结束 ---
        x = screen_width - width - margin_x
        y = screen_height - height - margin_y
        window.geometry(f'{width}x{height}+{x}+{y}')
        window.deiconify() # 显示窗口
        window.lift() # 确保在最前

        # 自动关闭 (仅对非可取消窗口)
        if not is_cancellable:
            # 使用 after 在 GUI 线程中安排销毁
            window.after(5000, lambda: safe_destroy(window))

    except tk.TclError as e:
         # 如果窗口在尝试操作时已被销毁，则忽略
         if "invalid command name" not in str(e):
              logging.error(f"创建或操作通知窗口时发生 TclError (GUI 线程): {e}", exc_info=True)
    except Exception as e:
        # 这个错误现在应该发生在 GUI 线程中
        logging.error(f"创建通知窗口时出错 (GUI 线程): {e}", exc_info=True)

def safe_destroy(widget):
    """安全地销毁 Tkinter 组件，避免因已销毁而出错"""
    try:
        if widget and widget.winfo_exists():
            widget.destroy()
    except tk.TclError as e:
        # 忽略 "invalid command name" 错误，这通常意味着组件已被销毁
        if "invalid command name" not in str(e):
            logging.warning(f"安全销毁组件时出错: {e}")
    except Exception as e:
        logging.warning(f"安全销毁组件时发生未知错误: {e}")


def process_notification_queue():
    """处理通知队列，在 GUI 线程中调度窗口创建"""
    global root_tk, stop_event, notification_queue
    logging.info("通知处理循环启动。")
    while not stop_event.is_set():
        try:
            # 使用带超时的 get，避免在停止时永久阻塞
            args = notification_queue.get(block=True, timeout=0.5) # 半秒超时
            if args is None: # 收到退出信号
                logging.info("通知队列收到退出信号。")
                break
            if root_tk:
                # 使用 root_tk.after() 将窗口创建调度到 GUI 线程执行
                # after(0, ...) 表示尽快执行
                root_tk.after(0, _create_notification_window, *args)
            else:
                logging.warning("尝试处理通知时 GUI 根窗口不存在。")
            notification_queue.task_done() # 标记任务完成
        except queue.Empty:
            # 超时，继续检查 stop_event
            continue
        except Exception as e:
             logging.error(f"处理通知队列时出错: {e}", exc_info=True)
             # 确保即使出错也标记任务完成，避免队列阻塞
             try: notification_queue.task_done()
             except ValueError: pass # 如果任务已被移除则忽略
    logging.info("通知处理循环结束。")


def show_custom_notification(title, message, is_cancellable=False, action_type=None, timeout=5):
    """将通知请求放入队列，由 GUI 线程处理"""
    logging.info(f"请求通知: {title} - {message[:100]}...") # 日志截断长消息
    global gui_thread, root_tk, notification_queue
    # 检查 GUI 是否准备就绪
    if not gui_thread or not gui_thread.is_alive() or root_tk is None:
         logging.warning("GUI 线程或根窗口未运行，无法显示自定义通知。将打印到控制台。")
         print(f"通知 (无GUI): {title} - {message}")
         return

    try:
        # 将参数放入队列
        # 注意：timeout 参数目前在 _create_notification_window 中处理 (控制非取消窗口的自动关闭时间)
        notification_queue.put((title, message, is_cancellable, action_type))
    except Exception as e:
        logging.error(f"放入通知队列时出错: {e}", exc_info=True)


# --- 系统操作 ---
def _execute_action(action_type):
    """实际执行关机或睡眠的函数"""
    action_name = "关机" if action_type == 'shutdown' else "睡眠"
    show_custom_notification(APP_NAME, f"正在执行{action_name}...", timeout=2)
    command = ""
    if action_type == 'shutdown':
        command = "shutdown /s /f /t 0" # 立即强制关机
    elif action_type == 'sleep':
        command = "rundll32.exe powrprof.dll,SetSuspendState 0,1,0" # 混合睡眠或睡眠

    if command and not run_command(command):
         show_custom_notification(f"{APP_NAME} 错误", f"{action_name}命令执行失败，请检查权限。")

def delayed_action_handler(action_type):
    """处理延迟操作和取消逻辑"""
    global pending_action_timer
    # 清除上一个计时器 (如果有)
    if pending_action_timer and pending_action_timer.is_alive():
        pending_action_timer.cancel()
        logging.info("上一个延迟操作已被新的请求覆盖。")

    cancel_action_event.clear() # 重置取消标志
    action_name = "关机" if action_type == 'shutdown' else "睡眠"
    logging.info(f"准备在 {ACTION_DELAY} 秒后执行 {action_name} 操作。")

    # 显示可取消的通知
    message = f"将在 {ACTION_DELAY} 秒后{action_name}，点击此通知可取消。"
    show_custom_notification(f"收到{action_name}指令", message, is_cancellable=True, action_type=action_type)

    # 定义延迟后要执行的任务
    def task():
        global pending_action_timer
        if not cancel_action_event.is_set():
            logging.info(f"延迟时间到，执行 {action_name} 操作。")
            _execute_action(action_type)
        else:
            logging.info(f"{action_name} 操作已被用户取消。")
            # 取消通知已在点击时显示，这里不再重复显示
        # 清理计时器引用
        pending_action_timer = None

    # 启动延迟计时器
    pending_action_timer = threading.Timer(ACTION_DELAY, task)
    pending_action_timer.name = f"{action_type.capitalize()}DelayTimer"
    pending_action_timer.start()

def shutdown_computer_request():
    """处理关机请求，启动延迟逻辑"""
    # 确保在单独线程中处理，避免阻塞 Flask 请求
    threading.Thread(target=delayed_action_handler, args=('shutdown',), name="ShutdownRequestHandler").start()

def sleep_computer_request():
    """处理睡眠请求，启动延迟逻辑"""
    threading.Thread(target=delayed_action_handler, args=('sleep',), name="SleepRequestHandler").start()

def sync_clipboard(text):
    """设置电脑的剪贴板内容，并显示普通通知"""
    if not isinstance(text, str):
        logging.warning("接收到无效的剪贴板数据 (非字符串)。")
        show_custom_notification(f"{APP_NAME} 警告", "接收到的剪贴板内容不是文本。")
        return False
    try:
        # 在单独线程中执行 pyperclip 操作，避免潜在的阻塞 GUI 或 Web 服务器
        def set_clipboard():
            try:
                pyperclip.copy(text)
                logging.info(f"剪贴板已同步。长度: {len(text)}")
                # 显示普通通知，包含同步的内容 (截断长内容)
                display_text = text if len(text) < 200 else text[:197] + "..."
                show_custom_notification("剪贴板已同步", display_text, action_type='clipboard')
            except pyperclip.PyperclipException as e_pyperclip:
                 # 特别处理 pyperclip 可能抛出的错误
                 logging.error(f"Pyperclip 设置剪贴板时出错: {e_pyperclip}", exc_info=True)
                 show_custom_notification(f"{APP_NAME} 剪贴板错误", f"无法访问或设置剪贴板: {e_pyperclip}\n可能是权限问题或剪贴板被占用。")
            except Exception as e:
                 logging.error(f"设置剪贴板时出错 (线程内): {e}", exc_info=True)
                 show_custom_notification(f"{APP_NAME} 错误", f"同步剪贴板失败: {e}")

        # 启动线程执行
        cb_thread = threading.Thread(target=set_clipboard, name="ClipboardSyncThread")
        cb_thread.start()
        return True # 假设启动线程成功

    except Exception as e:
        # 这个 catch 块是捕获启动线程时的错误
        logging.error(f"启动剪贴板同步线程失败: {e}", exc_info=True)
        show_custom_notification(f"{APP_NAME} 错误", f"启动同步剪贴板操作失败: {e}")
        return False

# --- Flask Web 服务器路由 ---
@app.route('/')
def index():
    """显示带有交互式按钮的首页"""
    # 使用更现代化的 HTML 和 CSS，添加 JavaScript 处理按钮点击
    html_content = """
    <!DOCTYPE html>
    <html lang="zh-CN">
    <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <title>{{ app_name }} 控制面板</title>
        <style>
            body {
                font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
                margin: 0;
                padding: 20px;
                background-color: #f0f2f5; /* 淡灰色背景 */
                color: #333;
                display: flex;
                flex-direction: column; /* 垂直排列 */
                align-items: center;
                min-height: 100vh;
            }
            .container {
                background-color: #ffffff;
                padding: 30px 40px;
                border-radius: 12px; /* 更圆的角 */
                box-shadow: 0 6px 12px rgba(0,0,0,0.1); /* 更明显的阴影 */
                width: 100%;
                max-width: 700px; /* 稍微加宽 */
                text-align: center;
                margin-bottom: 20px; /* 与页脚间距 */
            }
            h1 {
                color: #0056b3; /* 深蓝色 */
                margin-bottom: 25px;
                font-weight: 600;
            }
            .control-grid {
                display: grid;
                grid-template-columns: repeat(auto-fit, minmax(250px, 1fr)); /* 响应式网格 */
                gap: 25px; /* 网格间距 */
                margin-bottom: 30px;
            }
            .control-item {
                background-color: #f8f9fa; /* 项目背景色 */
                padding: 20px;
                border-radius: 8px;
                border: 1px solid #dee2e6; /* 浅边框 */
                display: flex;
                flex-direction: column;
                align-items: center;
                text-align: center;
            }
            .control-item h3 {
                margin-top: 0;
                margin-bottom: 10px;
                color: #495057; /* 标题颜色 */
            }
            .control-item p {
                font-size: 0.9em;
                color: #6c757d; /* 描述文字颜色 */
                line-height: 1.5;
                margin-bottom: 15px;
                min-height: 40px; /* 保证描述区域高度 */
            }
            button {
                background-color: #007bff; /* 蓝色按钮 */
                color: white;
                border: none;
                padding: 12px 20px; /* 按钮内边距 */
                border-radius: 6px;
                font-size: 1em;
                cursor: pointer;
                transition: background-color 0.2s ease, transform 0.1s ease;
                width: 100%; /* 按钮宽度 */
                max-width: 200px; /* 限制最大宽度 */
                margin-top: auto; /* 将按钮推到底部 */
            }
            button:hover {
                background-color: #0056b3; /* 深蓝色悬停 */
            }
            button:active {
                transform: scale(0.98); /* 点击效果 */
            }
            button.warning {
                background-color: #dc3545; /* 红色警告按钮 */
            }
            button.warning:hover {
                background-color: #c82333;
            }
            button.secondary {
                background-color: #6c757d; /* 灰色次要按钮 */
            }
            button.secondary:hover {
                background-color: #5a6268;
            }
            .clipboard-area {
                width: 100%;
                display: flex;
                flex-direction: column;
                align-items: center;
                gap: 10px; /* 输入框和按钮间距 */
            }
            input[type="text"] {
                width: 100%;
                padding: 10px;
                border: 1px solid #ced4da;
                border-radius: 6px;
                font-size: 0.95em;
                box-sizing: border-box; /* 防止 padding 撑大元素 */
            }
            .status-message {
                margin-top: 15px;
                font-weight: bold;
                min-height: 20px; /* 占位 */
            }
            .status-success { color: #28a745; } /* 绿色成功 */
            .status-error { color: #dc3545; } /* 红色错误 */
            .status-info { color: #17a2b8; } /* 蓝色信息 */

            .footer {
                margin-top: auto; /* 将页脚推到底部 */
                padding-top: 20px;
                font-size: 0.9em;
                color: #6c757d;
                text-align: center;
                width: 100%;
            }
        </style>
    </head>
    <body>
        <div class="container">
            <h1>{{ app_name }} 控制面板</h1>
            <div class="control-grid">
                <div class="control-item">
                    <h3>远程关机</h3>
                    <p>向电脑发送关机指令 (有 {{ delay }} 秒延迟，可在电脑端取消)。</p>
                    <button class="warning" onclick="sendCommand('/shutdown', '关机')">发送关机指令</button>
                </div>

                <div class="control-item">
                    <h3>远程睡眠</h3>
                    <p>向电脑发送睡眠指令 (有 {{ delay }} 秒延迟，可在电脑端取消)。</p>
                    <button class="secondary" onclick="sendCommand('/sleep', '睡眠')">发送睡眠指令</button>
                </div>

                <div class="control-item">
                    <h3>同步剪贴板 (输入)</h3>
                    <p>将下方输入的文本发送到电脑的剪贴板。</p>
                    <div class="clipboard-area">
                        <input type="text" id="clipboard-input" placeholder="在此输入文本...">
                        <button onclick="sendClipboardInput()">发送到电脑剪贴板</button>
                    </div>
                </div>

                <div class="control-item">
                    <h3>同步剪贴板 (粘贴)</h3>
                    <p>尝试读取您当前设备(手机/平板)的剪贴板内容，并发送到电脑。<br><strong>注意:</strong> 需要浏览器授权，且可能仅在 HTTPS 或 localhost 下有效。</p>
                    <button onclick="readClientClipboard()">尝试读取并发送</button>
                </div>
            </div>
            <div id="status-message" class="status-message"></div> </div>

        <div class="footer">
            <i>{{ app_name }} v{{ version }}</i>
        </div>

        <script>
            const statusDiv = document.getElementById('status-message');

            // 通用命令发送函数
            function sendCommand(endpoint, actionName) {
                statusDiv.textContent = \`正在发送\${actionName}指令...\`;
                statusDiv.className = 'status-message status-info'; // 设置为蓝色信息状态

                fetch(endpoint)
                    .then(response => {
                        if (response.ok) {
                            return response.text(); // 或者 response.json() 如果返回 JSON
                        } else {
                            throw new Error(\`\${actionName}请求失败: \${response.status} \${response.statusText}\`);
                        }
                    })
                    .then(data => {
                        console.log(\`\${actionName} 响应:\`, data);
                        statusDiv.textContent = \`\${actionName}指令已发送！请查看电脑端通知。\`;
                        statusDiv.className = 'status-message status-success'; // 绿色成功状态
                        // 可以设置延时清除消息
                        setTimeout(() => { statusDiv.textContent = ''; }, 5000);
                    })
                    .catch(error => {
                        console.error(\`发送\${actionName}指令时出错:\`, error);
                        statusDiv.textContent = \`发送\${actionName}指令失败: \${error.message}\`;
                        statusDiv.className = 'status-message status-error'; // 红色错误状态
                    });
            }

            // 发送输入框内容的函数
            function sendClipboardInput() {
                const textInput = document.getElementById('clipboard-input');
                const text = textInput.value;
                if (!text) {
                    statusDiv.textContent = '请输入要发送到剪贴板的文本！';
                    statusDiv.className = 'status-message status-error';
                    return;
                }

                // URL 编码文本以确保特殊字符能正确传输
                const encodedText = encodeURIComponent(text);
                const endpoint = \`/clip/\${encodedText}\`;

                statusDiv.textContent = '正在发送剪贴板内容...';
                statusDiv.className = 'status-message status-info';

                fetch(endpoint)
                    .then(response => {
                        if (response.ok) {
                            return response.text();
                        } else {
                            throw new Error(\`剪贴板同步失败: \${response.status} \${response.statusText}\`);
                        }
                    })
                    .then(data => {
                        console.log('剪贴板同步响应:', data);
                        statusDiv.textContent = '剪贴板内容已发送！';
                        statusDiv.className = 'status-message status-success';
                        textInput.value = ''; // 清空输入框
                        setTimeout(() => { statusDiv.textContent = ''; }, 5000);
                    })
                    .catch(error => {
                        console.error('发送剪贴板内容时出错:', error);
                        statusDiv.textContent = \`发送剪贴板内容失败: \${error.message}\`;
                        statusDiv.className = 'status-message status-error';
                    });
            }

            // 尝试读取客户端剪贴板并发送
            function readClientClipboard() {
                if (!navigator.clipboard || !navigator.clipboard.readText) {
                    statusDiv.textContent = '您的浏览器不支持或禁止读取剪贴板。';
                    statusDiv.className = 'status-message status-error';
                    return;
                }

                statusDiv.textContent = '正在请求剪贴板权限...';
                statusDiv.className = 'status-message status-info';

                navigator.clipboard.readText()
                    .then(text => {
                        if (!text) {
                            statusDiv.textContent = '剪贴板为空或无法读取。';
                            statusDiv.className = 'status-message status-info';
                            return;
                        }
                        console.log('从客户端剪贴板读取:', text);
                        statusDiv.textContent = '读取成功，正在发送...';
                        statusDiv.className = 'status-message status-info';

                        // 发送读取到的文本
                        const encodedText = encodeURIComponent(text);
                        const endpoint = \`/clip/\${encodedText}\`;

                        return fetch(endpoint); // 返回 fetch Promise
                    })
                    .then(response => {
                        // 这个 .then 处理 fetch 的响应
                        if (response && response.ok) {
                            return response.text();
                        } else if (response) {
                            throw new Error(\`发送剪贴板内容失败: \${response.status} \${response.statusText}\`);
                        }
                        // 如果没有 response (例如剪贴板为空时)，则不执行
                    })
                    .then(data => {
                         if (data !== undefined) { // 确保有数据才显示成功
                            console.log('发送客户端剪贴板响应:', data);
                            statusDiv.textContent = '已将您设备剪贴板的内容发送到电脑！';
                            statusDiv.className = 'status-message status-success';
                            setTimeout(() => { statusDiv.textContent = ''; }, 5000);
                         }
                    })
                    .catch(err => {
                        console.error('读取或发送客户端剪贴板时出错:', err);
                        let errorMsg = \`操作失败: \${err.message}\`;
                        if (err.name === 'NotAllowedError') {
                            errorMsg = '读取剪贴板失败：需要您的授权。';
                        } else if (err.message.includes('secure context')) {
                            errorMsg = '读取剪贴板失败：需要安全连接 (HTTPS) 或 localhost。';
                        }
                        statusDiv.textContent = errorMsg;
                        statusDiv.className = 'status-message status-error';
                    });
            }
        </script>
    </body>
    </html>
    """
    return render_template_string(html_content,
                                  port=SERVER_PORT,
                                  app_name=APP_NAME,
                                  version=APP_VERSION,
                                  delay=ACTION_DELAY)

# --- 其他路由保持不变 ---
@app.route('/ping', methods=['GET'])
def ping():
    """用于检查服务器是否存活的简单端点"""
    logging.info("收到 ping 请求。")
    return jsonify({"status": "ok", "message": f"{APP_NAME} server is running"}), 200

@app.route('/shutdown', methods=['GET'])
def handle_shutdown():
    """处理关机请求 (GET)，启动延迟和取消逻辑"""
    logging.info("收到关机请求 (GET)。")
    shutdown_computer_request() # 内部会启动线程
    # 返回 202 Accepted 表示请求已被接受处理
    return jsonify({"status": "accepted", "message": f"Shutdown command received. Action will be performed in {ACTION_DELAY} seconds unless cancelled."}), 202

@app.route('/sleep', methods=['GET'])
def handle_sleep():
    """处理睡眠请求 (GET)，启动延迟和取消逻辑"""
    logging.info("收到睡眠请求 (GET)。")
    sleep_computer_request() # 内部会启动线程
    return jsonify({"status": "accepted", "message": f"Sleep command received. Action will be performed in {ACTION_DELAY} seconds unless cancelled."}), 202

@app.route('/clip/<path:text_to_sync>', methods=['GET'])
def handle_clipboard(text_to_sync):
    """处理剪贴板同步请求 (GET, 从 URL 路径获取文本)"""
    # URL 解码由 Flask 自动完成
    logging.info(f"收到剪贴板同步请求 (GET)，文本长度: {len(text_to_sync)}")
    if sync_clipboard(text_to_sync): # 内部会启动线程
        return jsonify({"status": "accepted", "message": "Clipboard sync request sent."}), 202
    else:
        # 通常是启动线程失败
        return jsonify({"status": "error", "message": "Failed to initiate clipboard sync."}), 500


# --- 系统托盘图标 ---
def create_tray_image():
    """创建一个简单的系统托盘图标图像 (蓝色 X)"""
    width = 64
    height = 64
    # 使用白色背景，更通用
    image = Image.new('RGBA', (width, height), (255, 255, 255, 0)) # 透明背景
    dc = ImageDraw.Draw(image)
    # 画一个蓝色的圆角矩形边框
    dc.rounded_rectangle([5, 5, width - 6, height - 6], radius=10, outline='#007bff', width=6)
    # 画一个蓝色的 X
    line_width = 7
    dc.line([20, 20, width - 20, height - 20], fill='#007bff', width=line_width)
    dc.line([20, height - 20, width - 20, 20], fill='#007bff', width=line_width)
    return image

def is_startup_enabled():
    """检查应用程序是否已配置为开机自启"""
    try:
        # 使用 HKEY_CURRENT_USER，不需要管理员权限读取
        key = winreg.OpenKey(winreg.HKEY_CURRENT_USER, STARTUP_REG_KEY, 0, winreg.KEY_READ)
        winreg.QueryValueEx(key, APP_NAME)
        winreg.CloseKey(key)
        logging.debug("开机自启状态：已启用")
        return True
    except FileNotFoundError:
        logging.debug("开机自启状态：未启用 (注册表值不存在)")
        return False
    except Exception as e:
        logging.error(f"检查开机自启注册表时出错: {e}", exc_info=True)
        return False

def set_startup(enable):
    """启用或禁用应用程序开机自启"""
    # 修改注册表需要管理员权限
    if not is_admin():
         show_custom_notification(f"{APP_NAME} 权限错误", "需要管理员权限才能修改开机自启设置。\n请右键点击程序 -> 以管理员身份运行。")
         logging.warning("尝试在没有管理员权限的情况下修改开机自启设置。")
         return False

    try:
        # 确保使用 KEY_WRITE 权限打开
        key = winreg.OpenKey(winreg.HKEY_CURRENT_USER, STARTUP_REG_KEY, 0, winreg.KEY_WRITE)
        if enable:
            # 获取当前运行的 Python 解释器或打包后的 exe 路径
            executable_path = sys.executable
            # 如果是打包后的程序，sys.executable 就是 exe 路径
            # 如果是直接运行 .py 文件，sys.executable 是 python.exe 路径，需要加上脚本路径
            # 为了简化，我们假设用户会把打包后的 exe 或 .py 放在固定位置
            # 这里直接使用 sys.executable，对于打包的 exe 是正确的
            # 对于 .py，需要用户确保 python 在 PATH 中，或者提供 python.exe 的完整路径
            # 更健壮的方式是获取 .py 文件本身的路径: os.path.abspath(sys.argv[0])
            # 这里我们保持简单，用 sys.executable，并加上引号处理空格
            # 如果是运行 .py 文件，更推荐的方式是创建一个 .bat 或 .vbs 脚本来启动 python xxx.py
            # 这里我们假设用户运行的是打包后的 exe
            app_path = f'"{executable_path}"'
            winreg.SetValueEx(key, APP_NAME, 0, winreg.REG_SZ, app_path)
            logging.info(f"开机自启已启用。路径: {app_path}")
            show_custom_notification(APP_NAME, "已设置为开机自启。")
        else:
            try:
                winreg.DeleteValue(key, APP_NAME)
                logging.info("开机自启已禁用。")
                show_custom_notification(APP_NAME, "已取消开机自启。")
            except FileNotFoundError:
                logging.info("开机自启已经是禁用状态。")
            except Exception as e_del:
                logging.error(f"删除开机自启注册表值时出错: {e_del}", exc_info=True)
                show_custom_notification(f"{APP_NAME} Error", f"取消开机自启失败: {e_del}")
                winreg.CloseKey(key)
                return False
        winreg.CloseKey(key)
        return True
    except PermissionError as pe:
         logging.error(f"修改开机自启注册表权限不足: {pe}", exc_info=True)
         show_custom_notification(f"{APP_NAME} 权限错误", f"修改开机自启失败: 权限不足。\n请尝试以管理员身份运行。")
         return False
    except Exception as e:
        logging.error(f"修改开机自启注册表时发生未知错误: {e}", exc_info=True)
        show_custom_notification(f"{APP_NAME} Error", f"修改开机自启失败: {e}")
        # 尝试关闭 key，即使在出错时
        try: winreg.CloseKey(key)
        except Exception: pass
        return False

def on_quit_callback(icon, item):
    """当用户选择“退出”菜单项时的回调函数"""
    logging.info("从托盘菜单请求退出。")
    global stop_event, notification_queue, zeroconf_instance, tray_icon
    stop_event.set() # 设置停止标志
    notification_queue.put(None) # 发送信号让 GUI 队列处理器退出

    # 停止 Bonjour 服务 (需要 zeroconf 实例)
    if zeroconf_instance and service_info:
        logging.info("正在取消注册 Bonjour 服务...")
        try:
            # 在后台线程执行，避免阻塞退出流程
            threading.Thread(target=lambda z, si: (z.unregister_service(si), z.close()),
                             args=(zeroconf_instance, service_info),
                             name="ZeroconfStopThread", daemon=True).start() # 设置为 daemon
        except Exception as e:
            logging.error(f"取消注册 Bonjour 服务时出错: {e}", exc_info=True)

    # 停止系统托盘图标
    if tray_icon:
        logging.info("正在停止系统托盘图标...")
        # pystray 的 stop() 应该在主线程调用，或者由其内部事件循环处理
        # 这里假设 on_quit_callback 是在 pystray 的事件循环中调用的
        try:
            tray_icon.stop()
        except Exception as e:
            logging.error(f"停止托盘图标时出错: {e}") # 可能因为线程问题

    logging.info("退出回调执行完毕，主程序将开始清理。")


def on_toggle_startup_callback(icon, item):
    """当用户点击“开机自启”菜单项时的回调函数"""
    current_state = is_startup_enabled() # 获取当前实际状态
    logging.info(f"切换开机自启状态 (当前: {'启用' if current_state else '禁用'})。")
    try:
        set_startup(not current_state) # 尝试设置为相反状态
        # 更新菜单项状态 (pystray 会自动根据 checked 函数更新)
    except Exception as e:
        logging.error(f"切换开机自启时发生错误: {e}", exc_info=True)
        show_custom_notification(f"{APP_NAME} Error", f"切换开机自启失败: {e}")

def on_toggle_console_callback(icon, item=None):
    """当用户左键点击托盘图标或选择菜单项时的回调，用于显示/隐藏控制台"""
    global console_hwnd
    if not CAN_CONTROL_CONSOLE:
        logging.warning("无法控制控制台窗口 (pywin32 未安装或不支持)。")
        show_custom_notification(APP_NAME, "无法控制控制台窗口。\n(可能需要安装 pywin32)")
        return

    # 尝试获取控制台句柄 (可能在启动后才获取到)
    if console_hwnd == 0:
        console_hwnd = get_console_window()
        if console_hwnd == 0:
            # 可能是以 pythonw.exe 运行，没有控制台
            logging.info("未找到控制台窗口 (可能以无窗口模式 pythonw.exe 运行)。")
            show_custom_notification(APP_NAME, "未找到控制台窗口。\n(可能以无窗口模式运行)")
            return
        else:
             logging.info(f"获取到控制台窗口句柄: {console_hwnd}")

    try:
        # 检查窗口是否可见
        is_visible = win32gui.IsWindowVisible(console_hwnd)
        logging.debug(f"控制台窗口当前可见状态: {is_visible}")

        if is_visible:
            logging.info("隐藏控制台窗口。")
            win32gui.ShowWindow(console_hwnd, win32con.SW_HIDE)
        else:
            logging.info("显示控制台窗口。")
            win32gui.ShowWindow(console_hwnd, win32con.SW_SHOW)
            # 尝试将窗口带到前台
            try:
                # 先最小化再恢复，可以强制窗口到前台
                win32gui.ShowWindow(console_hwnd, win32con.SW_MINIMIZE)
                time.sleep(0.05) # 短暂延迟
                win32gui.ShowWindow(console_hwnd, win32con.SW_RESTORE) # 或者 SW_SHOW
                win32gui.SetForegroundWindow(console_hwnd)
            except Exception as e_fg:
                logging.warning(f"将控制台窗口设为前台失败: {e_fg}")

    except Exception as e:
        logging.error(f"切换控制台可见性时出错: {e}", exc_info=True)
        # 重置句柄，下次重新获取
        console_hwnd = 0


def setup_tray_icon():
    """创建并运行系统托盘图标 (应在主线程运行)"""
    global tray_icon
    try:
        image = create_tray_image()
        # 定义菜单项
        # 使用 lambda item: is_startup_enabled() 动态检查状态
        menu = (
             pystray.MenuItem('显示/隐藏控制台', on_toggle_console_callback, default=True), # 默认动作 (左键单击)
             pystray.MenuItem(
                '开机自启',
                on_toggle_startup_callback,
                checked=lambda item: is_startup_enabled() # 动态勾选状态
             ),
             pystray.Menu.SEPARATOR,
             pystray.MenuItem('退出', on_quit_callback)
        )
        tray_icon = pystray.Icon(
            APP_NAME,
            image,
            f"{APP_NAME} v{APP_VERSION}", # 鼠标悬停提示
            menu=menu
        )
        logging.info("正在启动系统托盘图标...")
        # tray_icon.run() 会阻塞，直到调用 icon.stop()
        tray_icon.run()
        # --- run() 返回后 ---
        logging.info("系统托盘图标已停止。")

    except Exception as e:
        logging.error(f"设置或运行系统托盘图标时出错: {e}", exc_info=True)
        # 如果托盘启动失败，也应该触发程序退出
        global stop_event, notification_queue
        stop_event.set()
        notification_queue.put(None) # 通知 GUI 线程退出

# --- Bonjour / mDNS 服务注册 ---
def register_bonjour_service():
    """使用 Zeroconf 注册服务"""
    global zeroconf_instance, service_info
    local_ip = get_local_ip()
    if local_ip == '127.0.0.1':
        logging.warning("未能获取到有效的局域网 IP 地址，Bonjour 服务可能无法被其他设备发现。将尝试使用 127.0.0.1 注册。")
        # Zeroconf 库可能仍然能够找到合适的接口进行广播

    logging.info(f"准备注册 Bonjour 服务 '{INSTANCE_NAME}' 在端口 {SERVER_PORT}")
    logging.info(f"尝试使用的 IP 地址: {local_ip} (Zeroconf 可能会选择其他接口)")

    try:
        # 显式指定 IPv4 Only
        zeroconf_instance = Zeroconf(ip_version=IPVersion.V4Only) # 让 Zeroconf 自动选择接口

        # 将 IP 地址转换为网络字节序
        packed_ip = socket.inet_aton(local_ip)

        # 创建 ServiceInfo
        # server=f"{socket.gethostname()}.local." 是 mDNS 标准方式指定服务器主机名
        service_info = ServiceInfo(
            type_=SERVICE_TYPE, # 服务类型
            name=INSTANCE_NAME, # 服务实例名
            addresses=[packed_ip], # 提供 IP 地址列表 (字节格式)
            port=SERVER_PORT,
            properties={'version': APP_VERSION, 'path': '/'}, # 可选属性
            server=f"{socket.gethostname()}.local." # 服务器的主机名.local
        )

        logging.info("正在注册 Bonjour 服务...")
        zeroconf_instance.register_service(service_info)
        logging.info(f"Bonjour 服务 '{INSTANCE_NAME}' 注册成功。其他设备现在可以通过 mDNS 发现此服务。")
        return True
    except OSError as ose:
        # 最常见的是端口 5353 被占用或权限问题
        if "address already in use" in str(ose) or "无法分配请求的地址" in str(ose) or "拒绝访问" in str(ose):
             logging.error(f"注册 Bonjour 服务失败 (OSError): {ose}. UDP 端口 5353 可能已被占用或被防火墙阻止。", exc_info=True)
             show_custom_notification(f"{APP_NAME} Bonjour 错误", f"无法注册服务: UDP 端口 5353 被占用或防火墙阻止。\n请检查 Apple Bonjour 服务或 iTunes 是否运行，或检查防火墙设置。")
        else:
             logging.error(f"注册 Bonjour 服务时发生 OSError: {ose}", exc_info=True)
             show_custom_notification(f"{APP_NAME} Bonjour 错误", f"注册服务时网络错误: {ose}")
        zeroconf_instance = None # 注册失败
        return False
    except Exception as e:
        logging.error(f"注册 Bonjour 服务时发生未知错误: {e}", exc_info=True)
        show_custom_notification(f"{APP_NAME} Bonjour 错误", f"注册服务失败: {e}\n请确认 Bonjour 服务已正确安装并运行。")
        zeroconf_instance = None
        return False


# --- 主应用程序逻辑 ---
def run_server():
    """运行 Flask Web 服务器 (在单独线程中)"""
    global stop_event
    try:
        logging.info(f"正在端口 {SERVER_PORT} 启动 Web 服务器 (使用 Waitress)...")
        from waitress import serve
        # 使用 Waitress 作为生产级 WSGI 服务器
        # serve 会阻塞，直到服务器停止
        serve(app, host='0.0.0.0', port=SERVER_PORT, threads=8)
    except ImportError:
        logging.warning("未找到 Waitress 库 (pip install waitress)，将使用 Flask 开发服务器 (仅供测试)。")
        try:
            # Flask 开发服务器不适合生产环境
            app.run(host='0.0.0.0', port=SERVER_PORT, debug=False, use_reloader=False)
        except OSError as e_dev:
            if "address already in use" in str(e_dev) or "无法绑定到请求的地址" in str(e_dev):
                 logging.error(f"端口 {SERVER_PORT} 已被占用 (Flask Dev Server)。")
                 show_custom_notification(f"{APP_NAME} 错误", f"端口 {SERVER_PORT} 已被占用！")
            else:
                 logging.error(f"启动 Flask 开发服务器失败: {e_dev}", exc_info=True)
                 show_custom_notification(f"{APP_NAME} 错误", f"无法启动网页服务器 (开发模式): {e_dev}")
            stop_event.set() # 触发程序退出
            notification_queue.put(None)
        except Exception as e_flask:
            logging.error(f"启动 Flask 开发服务器时发生未知错误: {e_flask}", exc_info=True)
            show_custom_notification(f"{APP_NAME} 错误", f"无法启动网页服务器: {e_flask}")
            stop_event.set()
            notification_queue.put(None)

    except OSError as e:
        if "address already in use" in str(e) or "无法绑定到请求的地址" in str(e):
             logging.error(f"端口 {SERVER_PORT} 已被占用 (Waitress)。")
             show_custom_notification(f"{APP_NAME} 错误", f"端口 {SERVER_PORT} 已被占用！请关闭使用该端口的其他程序或更换端口。")
        else:
             logging.error(f"启动 Web 服务器失败 (Waitress OSError): {e}", exc_info=True)
             show_custom_notification(f"{APP_NAME} 错误", f"无法启动网页服务器 (网络错误): {e}")
        stop_event.set() # 触发程序退出
        notification_queue.put(None)
    except Exception as e_waitress:
        logging.error(f"Web 服务器线程 (Waitress) 发生意外错误: {e_waitress}", exc_info=True)
        show_custom_notification(f"{APP_NAME} 严重错误", f"网页服务器意外停止: {e_waitress}")
        stop_event.set()
        notification_queue.put(None)
    finally:
        logging.info("Web 服务器线程已停止。")
        # 确保即使服务器异常退出，也设置停止事件
        if not stop_event.is_set():
            logging.warning("Web 服务器线程异常退出，设置停止事件。")
            stop_event.set()
            notification_queue.put(None)

def run_gui():
    """运行 Tkinter 主循环和处理通知队列 (在单独线程中)"""
    global root_tk, gui_thread, queue_processor_thread, stop_event
    try:
        logging.info("启动 GUI 线程...")
        root_tk = tk.Tk()
        root_tk.withdraw() # 隐藏主根窗口

        # 启动队列处理器线程
        queue_processor_thread = threading.Thread(target=process_notification_queue, name="NotificationQueueProcessor", daemon=True)
        queue_processor_thread.start()

        # ---- 运行 Tkinter 事件循环 ----
        # 使用 after 定时检查 stop_event 来优雅退出 mainloop
        def check_stop():
            global root_tk, stop_event
            if stop_event.is_set():
                logging.info("GUI 线程检测到停止信号，准备退出 Tkinter 循环。")
                if root_tk:
                    try:
                        root_tk.quit() # 退出 mainloop
                    except tk.TclError:
                        logging.warning("尝试退出 Tkinter 时出错 (可能已销毁)。")
            else:
                # 如果未停止，安排下一次检查
                if root_tk and root_tk.winfo_exists(): # 检查窗口是否存在
                    root_tk.after(200, check_stop) # 每 200ms 检查一次
                else:
                     logging.warning("GUI 根窗口不存在，停止 check_stop 循环。")


        # 启动第一次检查
        if root_tk:
            root_tk.after(200, check_stop)

        # 启动 Tkinter 主循环
        logging.info("GUI 线程进入 Tkinter mainloop。")
        root_tk.mainloop() # 这个调用会阻塞，直到 root_tk.quit() 执行

        # --- mainloop 结束后 ---
        logging.info("Tkinter mainloop 已退出。")

    except Exception as e:
        logging.error(f"GUI 线程出错: {e}", exc_info=True)
        stop_event.set() # 如果 GUI 线程崩溃，通知其他线程停止
        notification_queue.put(None)
    finally:
        logging.info("GUI 线程正在清理...")
        # 确保队列处理器线程有机会结束
        if queue_processor_thread and queue_processor_thread.is_alive():
            logging.info("等待通知队列处理器停止...")
            notification_queue.put(None) # 再发一次信号确保它能收到
            queue_processor_thread.join(timeout=1.0)
            if queue_processor_thread.is_alive():
                logging.warning("通知队列处理器未能优雅停止。")

        # 销毁根窗口 (如果还存在)
        if root_tk:
            logging.info("销毁 GUI 根窗口...")
            safe_destroy(root_tk)
        root_tk = None # 清理引用
        logging.info("GUI 线程已停止。")


def main():
    """主函数，协调启动和关闭"""
    global console_hwnd, gui_thread, server_thread, stop_event, zeroconf_instance, service_info

    # 初始设置
    if CAN_CONTROL_CONSOLE:
        console_hwnd = get_console_window()
        print(f"初始控制台窗口句柄: {console_hwnd if console_hwnd else '未找到或无窗口模式'}")

    setup_logging()

    if not is_admin():
        logging.warning("当前以非管理员权限运行。修改“开机自启”将需要管理员权限。")
        # 可以在这里弹出一个一次性的 Tkinter 提示窗口

    # 启动 GUI 线程 (必须先启动，以便其他部分可以显示通知)
    gui_thread = threading.Thread(target=run_gui, name="GuiThread")
    gui_thread.start()

    # 等待 GUI 初始化一点时间，确保 root_tk 可用
    time.sleep(1.5) # 稍微增加等待时间
    if not gui_thread or not gui_thread.is_alive() or root_tk is None:
        logging.critical("GUI 线程未能成功启动或初始化！自定义通知将不可用。")
        # 可以在这里决定是否继续运行，或者直接退出
        # show_custom_notification 在这种情况下会打印到控制台

    # 注册 Bonjour 服务
    bonjour_registered = register_bonjour_service()
    if not bonjour_registered:
        logging.warning("Bonjour 服务注册失败或未运行。本机服务仍可通过 IP 地址访问，但无法通过 .local 名称自动发现。")
        # 可以在这里显示一个通知
        show_custom_notification(f"{APP_NAME} 警告", "Bonjour 服务注册失败。\n服务发现功能可能受限。", timeout=10)

    # 启动 Web 服务器线程
    server_thread = threading.Thread(target=run_server, name="WebServerThread", daemon=True)
    server_thread.start()

    # 短暂等待后检查服务器线程是否成功启动
    time.sleep(2)
    if not server_thread.is_alive() and not stop_event.is_set():
         logging.critical("Web 服务器线程启动失败或过早退出！请检查日志（特别是端口占用错误）。")
         show_custom_notification(f"{APP_NAME} 严重错误", "Web 服务器未能启动！\n请检查控制台日志。")
         stop_event.set() # 触发清理流程
         notification_queue.put(None)
         # 等待 GUI 线程结束
         if gui_thread and gui_thread.is_alive(): gui_thread.join(timeout=2)
         # 取消注册 Bonjour (如果之前成功了)
         if bonjour_registered and zeroconf_instance and service_info:
             try: zeroconf_instance.unregister_service(service_info); zeroconf_instance.close()
             except Exception: pass
         sys.exit(1)

    # --- 在主线程运行系统托盘图标 ---
    # setup_tray_icon() 会阻塞，直到托盘图标退出
    logging.info("主线程进入系统托盘图标循环 (pystray)...")
    setup_tray_icon() # 这个函数包含了 tray_icon.run()

    # --- 程序退出流程 (当 setup_tray_icon 返回后) ---
    logging.info("主线程: 系统托盘图标已关闭或启动失败，开始全局清理...")
    if not stop_event.is_set():
        logging.warning("系统托盘意外退出，设置停止事件。")
        stop_event.set()
        notification_queue.put(None) # 确保 GUI 线程收到信号

    # 等待 Web 服务器线程停止
    if server_thread and server_thread.is_alive():
        logging.info("等待 Web 服务器线程完成...")
        # Waitress 是 daemon 线程，理论上会随主线程退出，但 join 可以等待清理
        server_thread.join(timeout=1.0) # 等待一小段时间
        if server_thread.is_alive():
            logging.warning("Web 服务器线程在超时后未能停止。")

    # 等待 GUI 线程停止
    if gui_thread and gui_thread.is_alive():
        logging.info("等待 GUI 线程停止...")
        # 确保队列处理器有机会退出
        if queue_processor_thread and queue_processor_thread.is_alive():
            queue_processor_thread.join(timeout=0.5)
        gui_thread.join(timeout=2.0) # 等待 GUI 线程完成清理
        if gui_thread.is_alive():
             logging.warning("GUI 线程在超时后未能优雅停止。")

    # Bonjour 服务应该已经在 on_quit_callback 中开始停止了

    logging.info(f"--- {APP_NAME} 已退出 ---")
    sys.exit(0)

if __name__ == '__main__':
    # 处理打包后的路径问题
    if getattr(sys, 'frozen', False):
        # 如果是 PyInstaller 打包的 .exe 文件
        application_path = os.path.dirname(sys.executable)
        os.chdir(application_path)
        print(f"程序以打包形式运行，工作目录设置为: {application_path}")
    else:
        # 如果是直接运行 .py 文件
        application_path = os.path.dirname(os.path.abspath(__file__))
        os.chdir(application_path)
        print(f"程序以脚本形式运行，工作目录设置为: {application_path}")


    if not CAN_CONTROL_CONSOLE:
        print("提示: 需要安装 pywin32 才能使用显示/隐藏控制台功能 (pip install pywin32)")

    # 启动主逻辑
    main()
