# -*- coding: utf-8 -*-

import logging
import threading
import time # 用于 sleep
from typing import Optional
import queue # **恢复：重新引入队列用于键盘信号**

# 导入必要的模块和函数
from . import config
from . import utils
from . import notifications # 用于显示通知
import pyperclip # 用于剪贴板操作
try:
    import keyboard # 导入 keyboard 库
    KEYBOARD_AVAILABLE = True
except ImportError:
    logging.warning("未找到 'keyboard' 库 (pip install keyboard)。将无法使用按键取消功能。")
    KEYBOARD_AVAILABLE = False
except Exception as e_kb_import:
    logging.error(f"导入 'keyboard' 库时出错: {e_kb_import}。将无法使用按键取消功能。")
    KEYBOARD_AVAILABLE = False


# --- 模块状态 ---
_pending_action_timer: Optional[threading.Timer] = None
_cancel_action_event = threading.Event()
_keyboard_listener_thread: Optional[threading.Thread] = None
_keyboard_cancel_queue = queue.Queue(maxsize=1) # **恢复：用于从键盘线程传递取消信号**
_stop_hook_flag = threading.Event() # 用于停止键盘 hook 的标志
_current_action_type: Optional[str] = None # 记录当前待定操作类型

# --- 核心操作执行 (代码无变化) ---
def _execute_action(action_type: str):
    """执行实际的关机或睡眠命令"""
    action_name = "关机" if action_type == 'shutdown' else "睡眠"
    # 发送 Bark 通知 (如果启用)
    if config.BARK_ENABLED:
         from . import bark_notifier # 局部导入避免循环
         sound = config.BARK_SOUND_SHUTDOWN if action_type == 'shutdown' else config.BARK_SOUND_SLEEP
         icon = config.BARK_ICON_SHUTDOWN if action_type == 'shutdown' else config.BARK_ICON_SLEEP
         bark_notifier.send_bark_notification(
             f"{config.HOSTNAME} 正在{action_name}",
             f"正在执行{action_name}命令...",
             group=config.BARK_GROUP_SYSTEM,
             sound=sound,
             icon=icon
         )
    # 显示桌面通知
    notifications.show_custom_notification(config.APP_NAME, f"正在执行{action_name}...")

    command = ""
    if action_type == 'shutdown':
        command = "shutdown /s /f /t 0"
    elif action_type == 'sleep':
        command = "rundll32.exe powrprof.dll,SetSuspendState 0,1,0"
    if command:
        success = utils.run_command(command)
        if not success:
            notifications.show_custom_notification(f"{config.APP_NAME} 错误", f"{action_name}命令执行失败，请检查权限。")
            # 发送失败的 Bark 通知
            if config.BARK_ENABLED:
                 bark_notifier.send_bark_notification(
                     f"{config.HOSTNAME} {action_name}失败",
                     f"{action_name}命令执行失败，请检查权限。",
                     group=config.BARK_GROUP_SYSTEM,
                     sound="failure"
                 )
    else:
        logging.warning(f"未定义操作类型 '{action_type}' 的命令。")


# --- 键盘监听与取消 ---
def _keyboard_listener_task():
    """在单独线程中运行，监听键盘按键并将信号放入队列"""
    global _keyboard_cancel_queue, _stop_hook_flag
    if not KEYBOARD_AVAILABLE:
        return

    logging.debug("键盘监听线程已启动，等待按键...")
    hook_id = None
    _stop_hook_flag.clear()

    try:
        def on_key_press(event):
            if event.event_type == keyboard.KEY_DOWN:
                logging.info(f"键盘监听器检测到按键按下: {event.name}")
                # **修改：只向队列发送信号，不直接调用取消**
                try:
                    _keyboard_cancel_queue.put_nowait(True)
                    logging.debug("键盘取消信号已放入队列。")
                except queue.Full:
                    logging.debug("键盘取消队列已满，忽略此次按键。")
                    pass
                # 设置停止标志，让监听循环退出
                _stop_hook_flag.set()

        hook_id = keyboard.hook(on_key_press, suppress=False)
        logging.debug(f"键盘钩子已设置 (ID: {hook_id})。")

        _stop_hook_flag.wait() # 等待停止信号
        logging.debug("键盘监听线程收到停止信号。")

    except ImportError:
        logging.error("运行时无法使用 keyboard 库。")
    except Exception as e:
        if "requires root" in str(e).lower() or isinstance(e, PermissionError):
             logging.error("键盘监听失败：需要管理员权限运行此程序才能全局监听键盘。")
        else:
             logging.error(f"键盘监听线程出错: {e}", exc_info=True)
    finally:
        # 尝试解除钩子，忽略 KeyError
        if hook_id:
            try:
                keyboard.unhook(hook_id)
                logging.debug(f"键盘钩子已解除 (ID: {hook_id})。")
            except KeyError:
                 logging.warning(f"尝试解除键盘钩子时发生 KeyError (ID: {hook_id})，可能已被移除。")
            except Exception as e_unhook:
                 logging.error(f"解除键盘钩子时发生未知错误: {e_unhook}")
        logging.debug("键盘监听线程已结束。")

def _stop_keyboard_listener():
    """停止键盘监听线程"""
    global _keyboard_listener_thread, _stop_hook_flag
    if KEYBOARD_AVAILABLE and _keyboard_listener_thread and _keyboard_listener_thread.is_alive():
        logging.debug("正在请求停止键盘监听线程...")
        _stop_hook_flag.set() # 设置停止标志

        # 清理队列中可能残留的信号
        while not _keyboard_cancel_queue.empty():
            try: _keyboard_cancel_queue.get_nowait()
            except queue.Empty: break

        # 等待线程结束
        _keyboard_listener_thread.join(timeout=0.5)
        if _keyboard_listener_thread.is_alive():
            logging.warning("键盘监听线程未能及时停止。")
        else:
            logging.debug("键盘监听线程已确认停止。")
    _keyboard_listener_thread = None


# --- 延迟操作处理 ---
def _delayed_action_handler(action_type: str):
    """处理关机/睡眠的延迟和取消逻辑"""
    global _pending_action_timer, _keyboard_listener_thread, _stop_hook_flag, _current_action_type

    # --- 清理上一次的操作 ---
    if _pending_action_timer and _pending_action_timer.is_alive():
        _pending_action_timer.cancel()
        logging.info("上一个延迟操作计时器已取消。")
    _stop_keyboard_listener()
    # -------------------------

    _cancel_action_event.clear()
    _current_action_type = action_type # 记录当前操作类型
    # 清空键盘取消队列
    while not _keyboard_cancel_queue.empty():
        try: _keyboard_cancel_queue.get_nowait()
        except queue.Empty: break

    action_name = "关机" if action_type == 'shutdown' else "睡眠"
    logging.info(f"计划在 {config.ACTION_DELAY} 秒后执行 {action_name} 操作。")

    # 显示可取消的通知 (提示文本更新)
    message = f"将在 {config.ACTION_DELAY} 秒后{action_name}，点击此通知或按任意键可取消。"
    notifications.show_custom_notification(f"收到{action_name}指令", message, is_cancellable=True, action_type=action_type)

    # 定义延迟后要执行的任务
    def task():
        global _pending_action_timer, _current_action_type
        # **修改：不再检查键盘队列，只检查主取消事件**
        if not _cancel_action_event.is_set():
            logging.info(f"延迟时间到，执行 {action_name} 操作。")
            _execute_action(action_type)
        # else: # 取消日志由 cancel_pending_action 处理

        _pending_action_timer = None
        _current_action_type = None # 清理当前操作类型
        _stop_keyboard_listener() # 确保键盘监听停止

    # 启动键盘监听线程
    if KEYBOARD_AVAILABLE and config.KEYBOARD_CANCEL_ENABLED:
        _keyboard_listener_thread = threading.Thread(target=_keyboard_listener_task, name="KeyboardListenerThread", daemon=True)
        _keyboard_listener_thread.start()
    elif config.KEYBOARD_CANCEL_ENABLED:
         logging.warning("Keyboard 库不可用或配置禁用，无法启动按键取消监听。")

    # 启动延迟计时器
    _pending_action_timer = threading.Timer(config.ACTION_DELAY, task)
    _pending_action_timer.name = f"{action_type.capitalize()}DelayTimer"
    _pending_action_timer.start()

def cancel_pending_action():
    """
    **修改：移除 source 参数。**
    由通知系统的回调函数调用以取消待定操作。
    """
    global _cancel_action_event, _pending_action_timer, _current_action_type
    if _pending_action_timer and _pending_action_timer.is_alive():
        action_name = "关机" if _current_action_type == 'shutdown' else "睡眠" if _current_action_type == 'sleep' else "操作"
        # **修改：移除 source 信息**
        logging.info(f"收到取消请求，取消待定的 {action_name} 操作。")

        _cancel_action_event.set() # 设置取消标志
        _pending_action_timer.cancel() # 取消计时器
        _pending_action_timer = None
        _stop_keyboard_listener() # 停止键盘监听

        # **修改：显示统一的取消通知（由 notifications.py 中的 _cancel_action 调用）**
        # cancel_source_text = "键盘按键" if source == "keyboard" else "用户点击" if source != "unknown" else "外部请求"
        # notifications.show_custom_notification(config.APP_NAME, f"{action_name} 操作已通过{cancel_source_text}取消。")

        _current_action_type = None # 清理当前操作类型
    else:
        # **修改：移除 source 信息**
        logging.debug(f"收到取消请求，但没有待定操作在进行中。")


# --- 公共函数 (无变化) ---
def shutdown_computer_request():
    """启动延迟关机过程"""
    threading.Thread(target=_delayed_action_handler, args=('shutdown',), name="ShutdownRequestHandler").start()

def sleep_computer_request():
    """启动延迟睡眠过程"""
    threading.Thread(target=_delayed_action_handler, args=('sleep',), name="SleepRequestHandler").start()

def sync_clipboard(text: str) -> bool:
    """在单独的线程中设置计算机的剪贴板内容。"""
    if not isinstance(text, str):
        logging.warning("收到的剪贴板同步数据类型无效（应为字符串）。")
        notifications.show_custom_notification(f"{config.APP_NAME} 警告", "接收到的剪贴板内容不是文本。")
        return False
    def set_clipboard_task():
        try:
            pyperclip.copy(text)
            log_text = text[:100] + "..." if len(text) > 100 else text
            logging.info(f"剪贴板同步成功。长度: {len(text)}, 内容: '{log_text}'")
            display_text = text if len(text) < 200 else text[:197] + "..."
            notifications.show_custom_notification("剪贴板已同步", display_text, action_type='clipboard')
            # 发送剪贴板同步成功的 Bark 通知
            if config.BARK_ENABLED:
                 from . import bark_notifier # 局部导入
                 bark_notifier.send_bark_notification(
                     f"{config.HOSTNAME} 剪贴板已同步",
                     f"内容: {display_text}",
                     group=config.BARK_GROUP_GENERAL,
                     sound=config.BARK_SOUND_CLIPBOARD,
                     icon=config.BARK_ICON_CLIPBOARD,
                     copy_text=text
                 )
        except pyperclip.PyperclipException as clip_err:
            logging.error(f"设置剪贴板时 Pyperclip 错误: {clip_err}", exc_info=True)
            notifications.show_custom_notification(f"{config.APP_NAME} 剪贴板错误", f"无法访问或设置剪贴板: {clip_err}\n可能是权限问题或剪贴板被其他应用锁定。")
        except Exception as e:
            logging.error(f"设置剪贴板时发生意外错误: {e}", exc_info=True)
            notifications.show_custom_notification(f"{config.APP_NAME} 错误", f"同步剪贴板时发生未知错误: {e}")
    try:
        cb_thread = threading.Thread(target=set_clipboard_task, name="ClipboardSyncThread")
        cb_thread.start()
        logging.debug("剪贴板同步任务已启动。")
        return True
    except Exception as e:
        logging.error(f"启动剪贴板同步线程失败: {e}", exc_info=True)
        notifications.show_custom_notification(f"{config.APP_NAME} 错误", f"无法启动剪贴板同步操作: {e}")
        return False
