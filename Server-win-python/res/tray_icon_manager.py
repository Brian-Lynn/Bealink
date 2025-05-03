# -*- coding: utf-8 -*-

import logging
import sys
import threading
import queue
import winreg
import time
from typing import Optional # 用于类型提示
from PIL import Image, ImageDraw
import pystray # type: ignore # pystray 可能没有现成的类型提示
import ctypes
import os # 用于在 _set_startup 中进行路径操作

# 导入必要的应用模块
from . import config
from . import utils
from . import notifications
from . import bonjour_manager # 用于在退出时注销服务

# 尝试导入 Windows 特定模块以控制控制台
try:
    import win32gui
    import win32con
    # 获取 GetConsoleWindow 函数指针
    get_console_window = ctypes.windll.kernel32.GetConsoleWindow
    CAN_CONTROL_CONSOLE = True
    logging.debug("找到 pywin32，控制台控制功能已启用。")
except (ImportError, AttributeError):
    logging.warning("未找到 pywin32 库或 GetConsoleWindow 不可用。控制台显示/隐藏功能已禁用。")
    CAN_CONTROL_CONSOLE = False
    # 如果无法控制，定义一个虚拟函数
    get_console_window = lambda: 0
    win32gui = None # type: ignore
    win32con = None # type: ignore
# 即使 pywin32 失败，ctypes 仍然需要（用于 utils 中的 is_admin 检查），所以无条件导入

# --- 模块状态 ---
_tray_icon: Optional[pystray.Icon] = None
_console_hwnd = 0 # 存储找到的控制台窗口句柄

# --- 托盘图标图像创建 ---
def _create_tray_image() -> Image.Image:
    """创建系统托盘图标的图像"""
    width = 64
    height = 64
    # 创建透明背景图像
    image = Image.new('RGBA', (width, height), (255, 255, 255, 0))
    dc = ImageDraw.Draw(image)

    # 绘制蓝色圆角矩形边框
    border_color = '#007bff' # 蓝色
    border_width = 6
    radius = 10
    dc.rounded_rectangle(
        [border_width // 2, border_width // 2, width - border_width // 2 -1, height - border_width // 2 -1],
        radius=radius, outline=border_color, width=border_width
    )

    # 在内部绘制蓝色 'X'
    line_width = 7
    margin = 20
    dc.line([(margin, margin), (width - margin, height - margin)], fill=border_color, width=line_width)
    dc.line([(margin, height - margin), (width - margin, margin)], fill=border_color, width=line_width)

    logging.debug("托盘图标图像已创建。")
    return image

# --- 启动项注册表管理 ---
def _is_startup_enabled() -> bool:
    """检查应用程序是否配置为在 Windows 启动时运行"""
    try:
        # 打开注册表项进行读取（CurrentUser 通常不需要管理员权限）
        key = winreg.OpenKey(winreg.HKEY_CURRENT_USER, config.STARTUP_REG_KEY, 0, winreg.KEY_READ)
        # 检查是否存在具有应用程序名称的值
        winreg.QueryValueEx(key, config.APP_NAME)
        winreg.CloseKey(key)
        logging.debug(f"启动项检查: 在 Run 键中找到 '{config.APP_NAME}'。已启用。")
        return True
    except FileNotFoundError:
        # 键或值未找到表示未启用
        logging.debug(f"启动项检查: 在 Run 键中未找到 '{config.APP_NAME}'。已禁用。")
        return False
    except Exception as e:
        logging.error(f"检查启动项注册表键时出错: {e}", exc_info=True)
        return False # 出错时假定为禁用

def _set_startup(enable: bool) -> bool:
    """启用或禁用应用程序在 Windows 启动时运行"""
    # 修改注册表需要管理员权限
    if not utils.is_admin():
        logging.warning("尝试在没有管理员权限的情况下修改启动项设置。")
        notifications.show_custom_notification(
            f"{config.APP_NAME} 权限错误",
            "需要管理员权限才能修改开机自启设置。\n请右键点击程序 -> 以管理员身份运行。"
        )
        return False

    try:
        # 以写入权限打开 Run 键
        key = winreg.OpenKey(winreg.HKEY_CURRENT_USER, config.STARTUP_REG_KEY, 0, winreg.KEY_WRITE)
        if enable:
            # 获取当前可执行文件的路径（适用于 .py 和冻结的 .exe）
            executable_path_raw = sys.executable
            # 用引号括起来以处理带空格的路径
            executable_path = f'"{executable_path_raw}"'

            # 如果作为脚本运行，为确保稳健性添加脚本路径参数
            if not getattr(sys, 'frozen', False) and '.py' in sys.argv[0]:
                 # 查找正在运行的主脚本的绝对路径 (main.py)
                 # 这假设 main.py 是作为脚本运行时的入口点
                 main_script_path = os.path.abspath(sys.argv[0]) # Python 被调用时使用的脚本路径
                 # 更健壮的方法是动态确定包入口点（可能很复杂）
                 # 为简单起见，假设 sys.argv[0] 指向我们的 main.py 或等效入口点
                 # 如果通过 `python -m` 运行，需要确保此路径正确
                 # 常见的模式是使用专用的启动器脚本或直接使用可执行文件路径
                 # 让我们优化一下：我们通常希望运行 *包*，而不仅仅是脚本文件。
                 # 使用 `python -m res.main` 是标准方式。
                 # 注册表项如果可能，应理想地反映这一点，或指向启动器。
                 # 为了简化直接运行脚本的情况，我们尝试：
                 try:
                     # 假设脚本作为 `python path/to/main.py` 运行
                     #script_dir = os.path.dirname(os.path.abspath(__file__)) # tray_icon_manager.py 的目录
                     # 假设 main.py 在同一目录中（或根据需要调整）
                     # main_py_path = os.path.join(script_dir, "main.py") # 或者动态确定入口点
                     # 使用 sys.argv[0] 通常更可靠，因为它就是被执行的文件
                     if os.path.exists(main_script_path):
                         # 命令变为："C:\path\to\python.exe" "C:\path\to\res\main.py"
                         executable_path = f'"{executable_path_raw}" "{main_script_path}"'
                         logging.debug(f"作为脚本运行，设置启动命令: {executable_path}")
                     else:
                          logging.warning(f"作为脚本运行时无法可靠确定主脚本路径以用于启动项。回退到可执行文件路径: {executable_path}")
                 except Exception as path_e:
                      logging.warning(f"确定启动项脚本路径时出错: {path_e}。回退到可执行文件路径: {executable_path}")


            winreg.SetValueEx(key, config.APP_NAME, 0, winreg.REG_SZ, executable_path)
            logging.info(f"开机自启已启用。命令设置为: {executable_path}")
            notifications.show_custom_notification(config.APP_NAME, "已设置为开机自启。")
        else:
            try:
                # 删除值以禁用启动项
                winreg.DeleteValue(key, config.APP_NAME)
                logging.info("开机自启已禁用。")
                notifications.show_custom_notification(config.APP_NAME, "已取消开机自启。")
            except FileNotFoundError:
                logging.info("开机自启已经是禁用状态。")
            except Exception as e_del:
                # 具体处理删除期间的错误
                logging.error(f"删除开机自启注册表值时出错: {e_del}", exc_info=True)
                notifications.show_custom_notification(f"{config.APP_NAME} Error", f"取消开机自启失败: {e_del}")
                winreg.CloseKey(key) # 即使出错也要确保关闭键
                return False
        winreg.CloseKey(key) # 成功关闭键
        return True
    except PermissionError:
        logging.error("修改启动项注册表时权限被拒绝。", exc_info=True)
        notifications.show_custom_notification(
            f"{config.APP_NAME} 权限错误",
            "修改开机自启失败: 权限不足。\n请以管理员身份运行程序。"
        )
        return False
    except Exception as e:
        logging.error(f"修改启动项注册表时发生意外错误: {e}", exc_info=True)
        notifications.show_custom_notification(f"{config.APP_NAME} Error", f"修改开机自启失败: {e}")
        # 即使在出错之前发生错误，也尝试关闭键
        try:
            if 'key' in locals() and key: winreg.CloseKey(key)
        except Exception: pass
        return False

# --- 托盘图标回调函数 ---
def _on_quit_callback(icon: pystray.Icon, item: pystray.MenuItem):
    """当点击“退出”菜单项时的回调函数"""
    logging.info("从托盘菜单请求退出。")
    # 1. 向主应用程序循环发出停止信号
    if _stop_event_ref:
        _stop_event_ref.set()
    # 2. 向通知队列处理器发出停止信号
    if _notification_queue_ref:
         try:
            _notification_queue_ref.put(None, block=False)
         except queue.Full:
             logging.warning("无法将停止信号放入通知队列（队列已满）。")
    # 3. 注销 Bonjour 服务（在后台线程中运行）
    bonjour_manager.unregister_service()
    # 4. 停止托盘图标本身（这允许 setup_tray_icon 返回）
    icon.stop()
    logging.info("托盘图标停止已启动。")

def _on_toggle_startup_callback(icon: pystray.Icon, item: pystray.MenuItem):
    """当点击“开机自启”菜单项时的回调函数"""
    current_state = _is_startup_enabled()
    logging.info(f"请求切换开机自启状态。当前状态: {'启用' if current_state else '禁用'}")
    # 尝试设置为相反状态
    success = _set_startup(not current_state)
    if not success:
        logging.warning("切换开机自启状态失败。")
    # 无需手动更新 item.checked，pystray 会通过 lambda 函数处理

def _on_toggle_console_callback(icon: Optional[pystray.Icon] = None, item: Optional[pystray.MenuItem] = None):
    """左键单击或点击“显示/隐藏控制台”菜单项的回调"""
    global _console_hwnd
    if not CAN_CONTROL_CONSOLE or not win32gui or not win32con:
        logging.warning("控制台控制依赖项 (pywin32) 不可用。")
        notifications.show_custom_notification(config.APP_NAME, "无法控制控制台窗口。\n(需要安装 pywin32)")
        return

    # 如果没有句柄或句柄可能已更改，则尝试获取控制台窗口句柄
    if _console_hwnd == 0:
        _console_hwnd = get_console_window()
        if _console_hwnd == 0:
            logging.info("找不到控制台窗口（可能使用 pythonw.exe 运行？）。")
            notifications.show_custom_notification(config.APP_NAME, "未找到控制台窗口。\n(可能以无窗口模式运行)")
            return # 没有句柄无法继续
        else:
            logging.info(f"获取到控制台窗口句柄: {_console_hwnd}")

    try:
        # 检查窗口当前是否可见
        is_visible = win32gui.IsWindowVisible(_console_hwnd)
        logging.debug(f"控制台窗口可见性: {is_visible}")

        if is_visible:
            logging.info("正在隐藏控制台窗口。")
            win32gui.ShowWindow(_console_hwnd, win32con.SW_HIDE)
        else:
            logging.info("正在显示控制台窗口。")
            win32gui.ShowWindow(_console_hwnd, win32con.SW_SHOW)
            # 尝试将控制台窗口置于前台
            try:
                # 有时最小化再恢复有助于将其置于前台
                win32gui.ShowWindow(_console_hwnd, win32con.SW_MINIMIZE)
                time.sleep(0.05) # 短暂延迟似乎有时有帮助
                win32gui.ShowWindow(_console_hwnd, win32con.SW_RESTORE)
                win32gui.SetForegroundWindow(_console_hwnd)
                logging.debug("尝试将控制台窗口置于前台。")
            except Exception as e_fg:
                # 由于 Windows 焦点窃取防护，这可能会失败
                logging.warning(f"将控制台窗口置于前台失败: {e_fg}")

    except Exception as e:
        # 处理窗口句柄变得无效时的潜在错误
        logging.error(f"切换控制台可见性时出错: {e}", exc_info=True)
        _console_hwnd = 0 # 如果发生错误则重置句柄，下次将尝试重新获取

# --- 托盘图标设置和运行 ---

# 存储回调函数所需的共享对象的引用
_stop_event_ref: Optional[threading.Event] = None
_notification_queue_ref: Optional[queue.Queue] = None

def setup_tray_icon(stop_event: threading.Event, notification_queue: queue.Queue):
    """创建并运行系统托盘图标。此函数会阻塞，直到图标停止。"""
    global _tray_icon, _stop_event_ref, _notification_queue_ref

    _stop_event_ref = stop_event
    _notification_queue_ref = notification_queue

    # 如果可能，尽早获取控制台句柄
    if CAN_CONTROL_CONSOLE:
        global _console_hwnd
        _console_hwnd = get_console_window()
        logging.debug(f"初始控制台窗口句柄: {_console_hwnd if _console_hwnd else '未找到'}")

    try:
        image = _create_tray_image()

        # 定义菜单结构
        # 'checked' lambda 函数动态确定复选标记状态
        menu = pystray.Menu(
            pystray.MenuItem(
                '显示/隐藏控制台',
                _on_toggle_console_callback,
                default=True, # 左键单击的操作
                enabled=CAN_CONTROL_CONSOLE # 如果缺少 pywin32 则禁用
            ),
            pystray.MenuItem(
                '开机自启',
                _on_toggle_startup_callback,
                checked=lambda item: _is_startup_enabled() # 动态检查状态
            ),
            pystray.Menu.SEPARATOR,
            pystray.MenuItem(
                '退出',
                _on_quit_callback
            )
        )

        # 创建 pystray Icon 对象
        _tray_icon = pystray.Icon(
            name=config.APP_NAME,
            icon=image,
            title=f"{config.APP_NAME} v{config.APP_VERSION}", # 工具提示文本
            menu=menu
        )

        logging.info("正在启动系统托盘图标...")
        # _tray_icon.run() 会阻塞调用线程（应为主线程）
        # 它处理图标事件，直到调用 icon.stop()（通常在 _on_quit_callback 中）
        _tray_icon.run()

        # --- icon.stop() 调用后，代码执行在此处恢复 ---
        logging.info("系统托盘图标运行循环已结束。")

    except Exception as e:
        logging.error(f"设置或运行系统托盘图标时发生致命错误: {e}", exc_info=True)
        # 确保如果托盘严重失败，应用程序会停止
        if _stop_event_ref:
            _stop_event_ref.set()
        if _notification_queue_ref:
             try:
                 _notification_queue_ref.put(None, block=False)
             except queue.Full:
                 pass
    finally:
        _tray_icon = None # 清除引用
        logging.info("托盘图标设置/运行函数已退出。")

