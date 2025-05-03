# -*- coding: utf-8 -*-

import tkinter as tk
from tkinter import ttk # 导入 ttk 用于 Progressbar
import queue # **恢复：导入 queue**
import logging
import threading
import time # 导入 time 用于精确计时
from typing import Optional, Tuple

# 导入配置和工具模块
from . import config
from . import utils
# 需要导入 system_actions 来检查和设置取消事件，以及访问键盘队列
from . import system_actions

# --- 模块状态 ---
_notification_queue: Optional[queue.Queue] = None
_root_tk: Optional[tk.Tk] = None
_gui_thread: Optional[threading.Thread] = None
_queue_processor_thread: Optional[threading.Thread] = None
_stop_event: Optional[threading.Event] = None

# --- 初始化和控制 (代码无变化) ---
def init_gui(stop_event_ref: threading.Event, queue_ref: queue.Queue):
    """初始化 Tkinter GUI 组件并启动 GUI 线程"""
    global _root_tk, _gui_thread, _stop_event, _notification_queue
    if _gui_thread and _gui_thread.is_alive():
        logging.warning("GUI 已经初始化。")
        return True
    _stop_event = stop_event_ref
    _notification_queue = queue_ref
    logging.info("正在初始化 Tkinter GUI 线程...")
    _gui_thread = threading.Thread(target=_run_gui_loop, name="GuiThread")
    _gui_thread.start()
    time.sleep(0.5)
    if not _gui_thread.is_alive() or _root_tk is None:
        logging.error("GUI 线程启动失败或未能初始化 Tkinter 根窗口。")
        return False
    logging.info("Tkinter GUI 线程启动成功。")
    return True

def stop_gui():
    """向 GUI 线程发送停止信号"""
    global _stop_event, _notification_queue
    logging.debug("请求停止 GUI。")
    if _stop_event: _stop_event.set()
    if _notification_queue:
        try: _notification_queue.put(None, block=False)
        except queue.Full: logging.warning("无法将停止信号放入通知队列（队列已满）。")

def join_gui_thread(timeout=2.0):
    """等待 GUI 线程和队列处理器终止"""
    global _gui_thread, _queue_processor_thread
    if _queue_processor_thread and _queue_processor_thread.is_alive():
        logging.debug("正在等待通知队列处理器线程结束...")
        _queue_processor_thread.join(timeout=0.5)
        if _queue_processor_thread.is_alive(): logging.warning("通知队列处理器线程未能优雅停止。")
        else: logging.debug("通知队列处理器线程已结束。")
    if _gui_thread and _gui_thread.is_alive():
        logging.debug(f"正在等待 GUI 线程结束 (超时: {timeout}s)...")
        _gui_thread.join(timeout=timeout)
        if _gui_thread.is_alive(): logging.warning("GUI 线程未能优雅停止。")
        else: logging.debug("GUI 线程已成功结束。")

def is_gui_running() -> bool:
    """检查 GUI 线程是否存活且 Tk 根窗口是否存在"""
    return bool(_gui_thread and _gui_thread.is_alive() and _root_tk)

# --- 核心 GUI 循环和通知处理 ---
def _run_gui_loop():
    """GUI 线程的主循环，处理 Tkinter 事件"""
    global _root_tk, _queue_processor_thread, _stop_event
    try:
        logging.info("GUI 线程已启动。正在初始化 Tkinter 根窗口...")
        _root_tk = tk.Tk()
        _root_tk.withdraw()
        # 配置 ttk 样式
        style = ttk.Style(_root_tk)
        try:
            available_themes = style.theme_names()
            logging.debug(f"可用的 ttk 主题: {available_themes}")
            if 'clam' in available_themes:
                style.theme_use('clam')
                logging.info("已切换 ttk 主题为 'clam' 以更好地支持样式。")
            else:
                 logging.warning("ttk 主题 'clam' 不可用，将使用默认主题，颜色可能不准确。")
            style.configure("red.Horizontal.TProgressbar",
                            troughcolor='#555555', background='#dc3545', thickness=8)
            logging.debug("ttk 样式 'red.Horizontal.TProgressbar' 已配置。")
        except tk.TclError as e_style:
             logging.error(f"配置 ttk 样式时出错: {e_style}")

        _queue_processor_thread = threading.Thread(target=_process_notification_queue, name="NotificationQueueProcessor", daemon=True)
        _queue_processor_thread.start()
        def check_stop():
            if _stop_event and _stop_event.is_set():
                logging.info("GUI 线程检测到停止信号。正在退出 Tkinter mainloop...")
                if _root_tk:
                    try: _root_tk.quit()
                    except Exception as e_quit: logging.warning(f"退出 mainloop 时出错: {e_quit}")
            elif _root_tk:
                 try: _root_tk.after(200, check_stop)
                 except Exception as e_after: logging.warning(f"调用 after 失败，可能窗口已销毁: {e_after}")
            else: logging.warning("Tkinter 根窗口不再存在，停止 check_stop。")
        logging.info("启动定期停止检查并进入 Tkinter mainloop。")
        if _root_tk:
            _root_tk.after(200, check_stop)
            _root_tk.mainloop()
        logging.info("Tkinter mainloop 已退出。")
    except Exception as e:
        logging.error(f"GUI 线程发生致命错误: {e}", exc_info=True)
        if _stop_event: _stop_event.set()
        if _notification_queue:
            try: _notification_queue.put(None, block=False)
            except queue.Full: pass
    finally:
        logging.info("GUI 线程正在清理...")
        if _queue_processor_thread and _queue_processor_thread.is_alive():
             logging.debug("等待通知队列处理器停止...")
             if _notification_queue: _notification_queue.put(None, block=False)
             _queue_processor_thread.join(timeout=0.5)
             if _queue_processor_thread.is_alive(): logging.warning("通知队列处理器未能优雅停止。")
        if _root_tk:
            logging.debug("销毁 Tkinter 根窗口。")
            utils.safe_destroy(_root_tk)
        _root_tk = None
        logging.info("GUI 线程已结束。")

def _process_notification_queue():
    """持续处理来自队列的通知请求"""
    global _notification_queue, _stop_event, _root_tk
    logging.info("通知队列处理器已启动。")
    while not (_stop_event and _stop_event.is_set()):
        try:
            args = _notification_queue.get(block=True, timeout=0.5)
            if args is None:
                logging.info("通知队列处理器收到停止信号。")
                break
            if _root_tk:
                _root_tk.after(0, lambda args_copy=args: _create_notification_window(*args_copy))
            else:
                logging.warning("无法创建通知窗口：Tkinter 根窗口不可用。")
            _notification_queue.task_done()
        except queue.Empty: continue
        except Exception as e:
            logging.error(f"处理通知队列时出错: {e}", exc_info=True)
            try:
                if _notification_queue: _notification_queue.task_done()
            except ValueError: pass
    logging.info("通知队列处理器已结束。")

# --- 用于显示通知的公共函数 ---
def show_custom_notification(title: str, message: str, is_cancellable: bool = False, action_type: Optional[str] = None):
    """将显示通知的请求添加到队列中。"""
    global _notification_queue
    log_message = message[:100] + "..." if len(message) > 100 else message
    logging.info(f"正在排队通知: 标题='{title}', 消息='{log_message}'")
    if not is_gui_running() or _notification_queue is None:
        logging.warning("GUI 未运行或队列未初始化。无法显示通知。将打印到控制台。")
        print(f"--- 通知 (GUI 不可用) ---\n标题: {title}\n消息: {message}\n-------------------------------------")
        return
    try:
        _notification_queue.put((title, message, is_cancellable, action_type))
    except Exception as e:
        logging.error(f"排队通知失败: {e}", exc_info=True)

# --- 通知窗口创建 (使用标准 Tkinter + 红色 LTR 进度条) ---
def _create_notification_window(title: str, message: str, is_cancellable: bool, action_type: Optional[str]):
    """创建并显示标准的 Tkinter 通知窗口。必须在 GUI 线程中运行。"""
    global _root_tk
    if not _root_tk:
        logging.error("无法创建通知窗口：Tkinter 根窗口不存在。")
        return

    window = None # 初始化 window 变量
    countdown_label: Optional[tk.Label] = None
    progress_bar: Optional[ttk.Progressbar] = None
    update_after_id: Optional[str] = None
    start_time: Optional[float] = None
    last_text_update_time: float = 0.0

    # --- 定义取消函数 (用于点击和键盘) ---
    def _cancel_action():
        nonlocal update_after_id # 引用外部函数的变量
        # **修改：移除 source 参数，统一处理**
        logging.info(f"请求取消 '{action_type}' 操作。")
        system_actions.cancel_pending_action() # 调用 system_actions 中的取消逻辑
        if update_after_id: # 取消计划的 after 调用
            try: window.after_cancel(update_after_id)
            except Exception: pass
            update_after_id = None
        utils.safe_destroy(window) # 安全销毁窗口
        # 显示取消通知
        action_name_cn = "关机" if action_type == 'shutdown' else "睡眠" if action_type == 'sleep' else "操作"
        show_custom_notification(config.APP_NAME, f"{action_name_cn} 操作已取消。")

    try:
        logging.debug(f"开始创建 Tkinter 通知窗口: Title='{title}'")
        window = tk.Toplevel(_root_tk)
        window.withdraw()
        window.overrideredirect(True)
        window.attributes('-topmost', True)
        window.attributes('-alpha', config.NOTIFICATION_ALPHA)
        logging.debug("Toplevel 窗口已创建。")

        window_padding = config.NOTIFICATION_PADDING
        title_font = (config.NOTIFICATION_TITLE_FONT_FAMILY, config.NOTIFICATION_TITLE_FONT_SIZE, "bold")
        message_font = (config.NOTIFICATION_MESSAGE_FONT_FAMILY, config.NOTIFICATION_MESSAGE_FONT_SIZE)
        min_width = 350

        window.minsize(width=min_width, height=0)
        logging.debug(f"窗口最小宽度设置为: {min_width}")

        frame = tk.Frame(window, bg=config.NOTIFICATION_BG_COLOR,
                         padx=window_padding, pady=window_padding,
                         highlightthickness=config.NOTIFICATION_CORNER_RADIUS_EFFECT,
                         highlightbackground=config.NOTIFICATION_BORDER_COLOR,
                         highlightcolor=config.NOTIFICATION_BORDER_COLOR)
        frame.pack(fill="both", expand=True)
        logging.debug("内部 Frame 已创建并打包。")

        lbl_title = tk.Label(frame, text=title, bg=config.NOTIFICATION_BG_COLOR, fg=config.NOTIFICATION_FG_COLOR,
                             font=title_font, wraplength=config.NOTIFICATION_WRAPLENGTH,
                             justify="left", anchor="w")
        lbl_title.pack(pady=(0, 5), fill="x")

        message_widget = None
        if action_type == 'clipboard' and len(message) > 150:
             text_message = tk.Text(frame, wrap=tk.WORD, height=config.NOTIFICATION_MAX_LINES, width=config.NOTIFICATION_WIDTH_CHARS, bg="#444444", fg=config.NOTIFICATION_FG_COLOR, font=message_font, relief=tk.FLAT, padx=5, pady=5, highlightthickness=0, borderwidth=0)
             text_message.insert(tk.END, message)
             text_message.config(state=tk.DISABLED)
             scrollbar = tk.Scrollbar(frame, command=text_message.yview, bg=config.NOTIFICATION_BG_COLOR, troughcolor="#555", activerelief=tk.FLAT, relief=tk.FLAT, width=12)
             text_message['yscrollcommand'] = scrollbar.set
             scrollbar.pack(side=tk.RIGHT, fill=tk.Y, padx=(5, 0))
             text_message.pack(side=tk.LEFT, fill=tk.BOTH, expand=True)
             message_widget = text_message
             logging.debug("使用 Text + Scrollbar 显示长消息。")
        else:
             if is_cancellable:
                 countdown_label = tk.Label(frame, text="", bg=config.NOTIFICATION_BG_COLOR, fg=config.NOTIFICATION_FG_COLOR,
                                           font=message_font, wraplength=config.NOTIFICATION_WRAPLENGTH,
                                           justify="left", anchor="nw")
                 countdown_label.pack(pady=(5, 5), fill="x")
                 message_widget = countdown_label
                 logging.debug("创建倒计时标签。")
             else:
                 lbl_message = tk.Label(frame, text=message, bg=config.NOTIFICATION_BG_COLOR, fg=config.NOTIFICATION_FG_COLOR,
                                       font=message_font, wraplength=config.NOTIFICATION_WRAPLENGTH,
                                       justify="left", anchor="nw")
                 lbl_message.pack(pady=(0, 0), fill="x")
                 message_widget = lbl_message
                 logging.debug("使用 Label 显示短消息。")

        if is_cancellable:
            progress_bar = ttk.Progressbar(frame, orient='horizontal', length=min_width - 2 * window_padding,
                                           mode='determinate', maximum=config.ACTION_DELAY * 1000,
                                           style="red.Horizontal.TProgressbar")
            progress_bar['value'] = 0
            progress_bar.pack(pady=(5, 0), fill='x')
            logging.debug("创建并打包进度条（红色样式，初始值0）。")

        # --- 平滑倒计时更新函数 ---
        def update_progress_and_text():
            nonlocal update_after_id, last_text_update_time
            current_time = time.monotonic()
            if start_time is None: return # 避免错误

            elapsed_time = current_time - start_time
            elapsed_time = min(elapsed_time, config.ACTION_DELAY)
            remaining_time = max(0.0, config.ACTION_DELAY - elapsed_time)

            try:
                # **修改：增加检查键盘取消队列**
                keyboard_cancel_requested = False
                try:
                    if not system_actions._keyboard_cancel_queue.empty():
                        system_actions._keyboard_cancel_queue.get_nowait() # 消耗信号
                        keyboard_cancel_requested = True
                except queue.Empty:
                    pass
                except AttributeError: # 如果 system_actions 未完全加载
                    pass

                # 检查窗口、主取消事件或键盘取消信号
                if not window or not window.winfo_exists() or system_actions._cancel_action_event.is_set() or keyboard_cancel_requested:
                    log_reason = "窗口关闭" if not window or not window.winfo_exists() else \
                                 "键盘取消" if keyboard_cancel_requested else "事件取消"
                    logging.debug(f"平滑倒计时停止 ({log_reason})。")
                    if update_after_id:
                        try: window.after_cancel(update_after_id)
                        except Exception: pass
                    update_after_id = None
                    # **修改：如果是由键盘触发的，调用 _cancel_action 来统一处理**
                    if keyboard_cancel_requested:
                         _cancel_action() # 模拟点击取消
                    return

                # 更新进度条
                if progress_bar:
                    progress_bar['value'] = elapsed_time * 1000

                # 更新文本标签（大约每秒一次）
                if countdown_label and (current_time - last_text_update_time >= 0.95):
                    seconds_left_display = int(remaining_time + 0.5)
                    action_name_cn = "关机" if action_type == 'shutdown' else "睡眠" if action_type == 'sleep' else "操作"
                    if seconds_left_display > 0:
                        countdown_label.config(text=f"将在 {seconds_left_display} 秒后{action_name_cn}，点击取消")
                    else:
                        countdown_label.config(text=f"正在执行{action_name_cn}...")
                    last_text_update_time = current_time

                # 安排下一次更新
                if elapsed_time < config.ACTION_DELAY:
                    update_after_id = window.after(17, update_progress_and_text)
                else:
                    logging.debug("平滑倒计时结束。")
                    if progress_bar: progress_bar['value'] = progress_bar['maximum']
                    update_after_id = None
                    window.after(200, lambda w=window: utils.safe_destroy(w))

            except Exception as e_update:
                logging.error(f"更新平滑倒计时时出错: {e_update}", exc_info=False)
                if update_after_id:
                     try: window.after_cancel(update_after_id)
                     except Exception: pass
                     update_after_id = None

        # --- 取消逻辑绑定 ---
        if is_cancellable:
            # **修改：绑定 _cancel_action 函数**
            clickable_widgets = [frame, lbl_title]
            if message_widget: clickable_widgets.append(message_widget)
            if progress_bar: clickable_widgets.append(progress_bar)

            for widget in clickable_widgets:
                # 使用 lambda 捕获当前的 _cancel_action 引用
                widget.bind("<Button-1>", lambda event, func=_cancel_action: func())
                widget.config(cursor="hand2")
            logging.debug("取消逻辑已绑定到点击事件。")

            # 启动平滑倒计时
            start_time = time.monotonic()
            last_text_update_time = start_time
            update_progress_and_text()
            logging.debug("平滑倒计时已启动。")

        # --- 窗口尺寸和定位 ---
        window.update_idletasks()
        width = window.winfo_width()
        height = window.winfo_height()
        width = max(width, min_width)
        logging.debug(f"获取到窗口尺寸: Width={width}, Height={height}")

        screen_width = window.winfo_screenwidth()
        screen_height = window.winfo_screenheight()
        x_pos = screen_width - width - config.NOTIFICATION_MARGIN_X
        y_pos = screen_height - height - config.NOTIFICATION_MARGIN_Y
        window.geometry(f'{width}x{height}+{x_pos}+{y_pos}')
        logging.debug(f"窗口定位至: x={x_pos}, y={y_pos}")

        # --- 自动关闭 (仅非取消窗口) ---
        if not is_cancellable:
            window.after(config.NOTIFICATION_TIMEOUT, lambda w=window: utils.safe_destroy(w))
            logging.debug(f"已设置 {config.NOTIFICATION_TIMEOUT}ms 后自动关闭。")

        # --- 显示窗口 ---
        window.deiconify()
        window.lift()
        logging.info(f"通知窗口 '{title}' 创建并显示成功。")

    except Exception as e:
        logging.error(f"创建 Tkinter 通知窗口时发生错误: {e}", exc_info=True)
        if window: utils.safe_destroy(window)
