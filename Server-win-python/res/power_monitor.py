# -*- coding: utf-8 -*-

import logging
import threading
import sys
import time
from typing import Optional # **新增：导入 Optional**

try:
    import win32con
    import win32gui
    import win32api
    PYWIN32_AVAILABLE = True
except ImportError:
    logging.warning("未找到 'pywin32' 库 (pip install pywin32)。将无法监听电源事件 (睡眠/唤醒)。")
    PYWIN32_AVAILABLE = False

# 导入 Bark 通知函数 (如果可用)
try:
    from . import bark_notifier # **修改：只导入模块**
    from . import config
    # **修改：BARK_AVAILABLE 由 bark_notifier 模块定义**
    # BARK_AVAILABLE = True # 移除这行
    CONFIG_AVAILABLE = True # 标记 config 导入成功
except ImportError:
    logging.warning("无法导入 bark_notifier 或 config，电源事件通知将不可用。")
    # **修改：将模块设为 None，并标记为不可用**
    bark_notifier = None
    config = None
    # BARK_AVAILABLE = False # 移除这行
    CONFIG_AVAILABLE = False


# --- 全局变量 ---
_monitor_thread: Optional[threading.Thread] = None
_hwnd: Optional[int] = None # 隐藏窗口的句柄
_stop_monitor_event = threading.Event() # 用于通知监控线程停止

# --- Windows 消息处理函数 ---
def _wnd_proc(hWnd: int, msg: int, wParam: int, lParam: int):
    """处理 Windows 消息，特别是电源相关的消息"""
    if PYWIN32_AVAILABLE and msg == win32con.WM_POWERBROADCAST:
        event_name = "未知电源事件"
        send_notification = False
        hostname = getattr(config, 'HOSTNAME', '电脑') if CONFIG_AVAILABLE else '电脑'

        if wParam == win32con.PBT_APMSUSPEND:
            event_name = "PBT_APMSUSPEND (系统即将睡眠)"
            logging.info(f"检测到电源事件: {event_name}")
            # **修改：检查 bark_notifier.BARK_AVAILABLE**
            if bark_notifier and bark_notifier.BARK_AVAILABLE and CONFIG_AVAILABLE and config.BARK_ENABLED:
                bark_notifier.send_bark_notification(
                    f"{hostname} 即将睡眠",
                    "系统正在进入睡眠或休眠状态...",
                    group=config.BARK_GROUP_SYSTEM,
                    icon=config.BARK_ICON_SLEEP,
                    sound=config.BARK_SOUND_SLEEP
                    )
            send_notification = True

        elif wParam == win32con.PBT_APMRESUMEAUTOMATIC:
            event_name = "PBT_APMRESUMEAUTOMATIC (系统自动唤醒)"
            logging.info(f"检测到电源事件: {event_name}")
             # **修改：检查 bark_notifier.BARK_AVAILABLE**
            if bark_notifier and bark_notifier.BARK_AVAILABLE and CONFIG_AVAILABLE and config.BARK_ENABLED:
                bark_notifier.send_bark_notification(
                    f"{hostname} 已唤醒",
                    "系统已从睡眠或休眠状态恢复。",
                    group=config.BARK_GROUP_SYSTEM,
                    icon=config.BARK_ICON_WAKE,
                    sound=config.BARK_SOUND_WAKE
                    )
            send_notification = True

        elif wParam == win32con.PBT_APMRESUMESUSPEND:
            event_name = "PBT_APMRESUMESUSPEND (睡眠尝试后恢复)"
            logging.info(f"检测到电源事件: {event_name}")
            send_notification = True

        elif wParam == win32con.PBT_POWERSETTINGCHANGE:
            event_name = "PBT_POWERSETTINGCHANGE (电源设置更改)"
            logging.debug(f"检测到电源事件: {event_name}")
            pass

        else:
            logging.debug(f"收到未处理的 WM_POWERBROADCAST 事件: wParam={wParam}")
            pass

    elif PYWIN32_AVAILABLE and msg == win32con.WM_DESTROY:
        logging.info("电源监控窗口收到 WM_DESTROY 消息，正在停止消息循环。")
        win32gui.PostQuitMessage(0)
        return 0

    elif PYWIN32_AVAILABLE and msg == win32con.WM_CLOSE:
         logging.info("电源监控窗口收到 WM_CLOSE 消息，正在停止。")
         win32gui.PostQuitMessage(0)
         return 0

    if PYWIN32_AVAILABLE:
        return win32gui.DefWindowProc(hWnd, msg, wParam, lParam)
    else:
        return 0

# --- 创建和运行隐藏窗口的函数 ---
def _create_hidden_window():
    """创建用于接收系统消息的隐藏窗口并运行消息循环"""
    global _hwnd
    if not PYWIN32_AVAILABLE:
        logging.error("无法创建电源监控窗口，因为 pywin32 不可用。")
        return

    logging.info("正在创建隐藏的电源监控窗口...")
    hInstance = None
    classAtom = None
    wc = None
    try:
        wc = win32gui.WNDCLASS()
        hInstance = wc.hInstance = win32api.GetModuleHandle(None)
        wc.lpszClassName = "BealinkPowerMonitorClass"
        wc.lpfnWndProc = _wnd_proc

        try:
             classAtom = win32gui.RegisterClass(wc)
        except win32gui.error as e:
             if e.winerror == 1410: # ERROR_CLASS_ALREADY_EXISTS
                 logging.warning("窗口类 'BealinkPowerMonitorClass' 已存在，尝试注销并重新注册...")
                 try:
                     win32gui.UnregisterClass(wc.lpszClassName, hInstance)
                     classAtom = win32gui.RegisterClass(wc)
                 except Exception as e_re_register:
                     logging.error(f"重新注册窗口类失败: {e_re_register}")
                     return
             else:
                 raise

        if not classAtom:
            logging.error("注册电源监控窗口类失败。")
            return

        _hwnd = win32gui.CreateWindow(
            wc.lpszClassName, "BealinkPowerMonitorWindow", 0,
            0, 0, 0, 0, 0, 0, hInstance, None
        )

        if not _hwnd:
            logging.error("创建电源监控窗口失败。")
            if classAtom and hInstance: win32gui.UnregisterClass(wc.lpszClassName, hInstance)
            return

        logging.info(f"电源监控窗口创建成功 (HWND: {_hwnd})。进入消息循环...")
        win32gui.PumpMessages() # 阻塞直到 PostQuitMessage
        logging.info("电源监控消息循环已退出。正在销毁窗口...")

    except Exception as e:
        logging.error(f"电源监控线程发生错误: {e}", exc_info=True)
    finally:
        # 清理逻辑
        current_hwnd = _hwnd # 在 finally 块开始时捕获句柄
        if current_hwnd:
            try:
                win32gui.DestroyWindow(current_hwnd)
                logging.debug("电源监控窗口已销毁。")
            except Exception as e_destroy:
                 logging.warning(f"销毁电源监控窗口时出错: {e_destroy}")
        _hwnd = None # 确保重置

        # 仅当成功注册且获取到 hInstance 时才尝试注销
        if classAtom and hInstance and wc:
             try:
                 win32gui.UnregisterClass(wc.lpszClassName, hInstance)
                 logging.debug("电源监控窗口类已注销。")
             except Exception as e_unregister:
                  logging.warning(f"注销电源监控窗口类时出错: {e_unregister}")
        logging.info("电源监控线程已结束。")


# --- 公共函数：启动和停止监控 ---
def start_power_monitoring():
    """在单独的线程中启动电源事件监控"""
    global _monitor_thread
    if not PYWIN32_AVAILABLE:
        logging.warning("无法启动电源监控，因为 pywin32 不可用。")
        return

    if _monitor_thread and _monitor_thread.is_alive():
        logging.warning("电源监控线程已在运行中。")
        return

    logging.info("正在启动电源监控线程...")
    _stop_monitor_event.clear()
    _monitor_thread = threading.Thread(target=_create_hidden_window, name="PowerMonitorThread")
    _monitor_thread.start()

def stop_power_monitoring():
    """停止电源事件监控线程"""
    global _monitor_thread, _hwnd
    if not PYWIN32_AVAILABLE:
        return

    if _monitor_thread and _monitor_thread.is_alive():
        logging.info("正在停止电源监控线程...")
        _stop_monitor_event.set()

        current_hwnd = _hwnd # 在访问前捕获句柄值
        if current_hwnd:
            try:
                logging.debug(f"向电源监控窗口 (HWND: {current_hwnd}) 发送 WM_CLOSE 消息。")
                win32api.PostMessage(current_hwnd, win32con.WM_CLOSE, 0, 0)
            except Exception as e_post:
                 logging.error(f"发送 WM_CLOSE 消息到电源监控窗口失败: {e_post}")
        else:
             logging.warning("无法发送 WM_CLOSE，因为电源监控窗口句柄未知或已失效。")

        _monitor_thread.join(timeout=2.0)
        if _monitor_thread.is_alive():
            logging.warning("电源监控线程未能优雅停止。")
        else:
            logging.info("电源监控线程已停止。")
    else:
        logging.debug("电源监控线程未运行或已停止。")

    _monitor_thread = None
    _hwnd = None

