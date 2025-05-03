# -*- coding: utf-8 -*-

import threading
import time
import sys
import os
import logging
import queue # 用于创建共享队列
import traceback # 导入 traceback 模块以打印详细错误信息

# --- 导入应用模块 ---
try:
    from . import config
    from . import utils
    from . import notifications
    from . import system_actions
    from . import web_server
    from . import tray_icon_manager
    from . import bonjour_manager
    # **新增：导入 Bark 和电源监控模块**
    from . import bark_notifier
    from . import power_monitor
except ImportError as e:
    logging.basicConfig(level=logging.DEBUG, format='%(asctime)s - %(levelname)s - %(threadName)s - %(message)s')
    logging.error(f"相对导入失败: {e}。尝试绝对导入。")
    try:
        import config
        import utils
        import notifications
        import system_actions
        import web_server
        import tray_icon_manager
        import bonjour_manager
        # **新增：导入 Bark 和电源监控模块**
        import bark_notifier
        import power_monitor
    except ImportError as e2:
         logging.critical(f"绝对导入也失败: {e2}。无法继续。")
         print("--- 严重导入错误 ---", file=sys.stderr)
         traceback.print_exc()
         print("-----------------------------", file=sys.stderr)
         input("按 Enter 键退出...")
         sys.exit(1)


# --- 全局共享对象 ---
stop_event = threading.Event()
notification_queue = queue.Queue()

# --- 主应用逻辑 ---
def main():
    """初始化并运行应用程序组件的主函数"""
    global stop_event, notification_queue

    # 1. 初始设置
    utils.setup_logging(debug_mode=config.DEBUG)
    logging.info(f"--- 启动 {config.APP_NAME} v{config.APP_VERSION} ---")

    if getattr(sys, 'frozen', False):
        application_path = os.path.dirname(sys.executable)
        try:
            os.chdir(application_path)
            logging.info(f"作为冻结可执行文件运行。工作目录设置为: {application_path}")
        except Exception as e_chdir:
            logging.error(f"为冻结可执行文件更改工作目录失败: {e_chdir}")
    else:
        application_path = os.path.dirname(os.path.abspath(__file__))
        logging.info(f"作为脚本运行。应用路径: {application_path}")

    if not utils.is_admin():
        logging.warning("以非管理员权限运行。修改“开机自启”将失败。")
        if config.KEYBOARD_CANCEL_ENABLED:
             logging.warning("键盘取消功能可能也需要管理员权限才能全局生效。")

    # 2. 初始化 GUI 通知系统
    logging.info("正在初始化 GUI 通知系统...")
    gui_started = notifications.init_gui(stop_event, notification_queue)
    if not gui_started:
        logging.critical("GUI 通知系统初始化失败！将无法显示通知。")
    else:
        time.sleep(0.2)

    # 启动电源事件监控线程 (如果可用)
    if power_monitor.PYWIN32_AVAILABLE:
        power_monitor.start_power_monitoring()
    else:
         logging.warning("pywin32 不可用，无法启动电源事件监控。")


    # 3. 注册 Bonjour/mDNS 服务
    logging.info("正在注册 Bonjour 服务...")
    bonjour_registered = bonjour_manager.register_service()
    if not bonjour_registered:
        logging.warning("Bonjour 服务注册失败。网络发现 via .local name will not work.")

    # 4. 启动 Web 服务器线程
    logging.info("正在启动 Web 服务器线程...")
    server_thread = threading.Thread(
        target=web_server.run_server,
        args=(stop_event,),
        name="WebServerThread",
        daemon=True
    )
    server_thread.start()

    time.sleep(1.0)
    if not server_thread.is_alive() and not stop_event.is_set():
        logging.critical("Web 服务器线程启动失败或过早退出！")
        stop_event.set()
        notifications.stop_gui()
        bonjour_manager.unregister_service()
        power_monitor.stop_power_monitoring()
        notifications.join_gui_thread()
        raise RuntimeError("Web 服务器启动失败。")
    else:
         logging.info(f"Web 服务器线程已启动。访问 http://<你的IP>:{config.SERVER_PORT} 或 http://{config.HOSTNAME}.local:{config.SERVER_PORT}")
         # 发送启动成功的 Bark 通知
         # **修改：检查 bark_notifier.BARK_AVAILABLE**
         if bark_notifier and bark_notifier.BARK_AVAILABLE and config.BARK_ENABLED:
              bark_notifier.send_bark_notification(
                  f"{config.HOSTNAME} 上的 {config.APP_NAME} 已启动", # **修改：更明确的标题**
                  f"服务器已成功启动并监听在端口 {config.SERVER_PORT}。",
                  group=config.BARK_GROUP_SYSTEM, # 使用系统分组
                  icon=config.BARK_ICON_STARTUP,  # 使用启动图标
                  sound=config.BARK_SOUND_STARTUP # 使用启动声音
              )


    # 5. 启动系统托盘图标 (阻塞主线程)
    logging.info("正在启动系统托盘图标（将阻塞主线程）...")
    try:
        tray_icon_manager.setup_tray_icon(stop_event, notification_queue)
    except Exception as e_tray:
         logging.critical(f"设置/运行托盘图标失败: {e_tray}", exc_info=True)
         stop_event.set()

    # --- 应用程序关闭序列 ---
    logging.info("主线程：托盘图标已退出或失败。正在启动关闭程序...")

    if not stop_event.is_set():
        logging.warning("托盘图标意外退出。正在设置停止事件。")
        stop_event.set()
        notifications.stop_gui()

    # 停止电源监控线程
    logging.info("正在停止电源监控...")
    power_monitor.stop_power_monitoring()

    # 等待 Web 服务器线程
    if server_thread and server_thread.is_alive():
        logging.info("正在短暂等待 Web 服务器线程完成...")
        server_thread.join(timeout=0.5)
        if server_thread.is_alive(): logging.warning("Web 服务器线程未能快速退出。")

    # 等待 GUI 线程
    logging.info("正在等待 GUI 线程停止...")
    notifications.join_gui_thread(timeout=2.0)

    # Bonjour 服务已在托盘退出回调中注销

    logging.info(f"--- {config.APP_NAME} 已结束 ---")


# --- 脚本入口点 ---
if __name__ == '__main__':
    try:
        # 检查依赖项
        # **修改：检查 bark_notifier.BARK_AVAILABLE**
        if 'tray_icon_manager' in sys.modules and not tray_icon_manager.CAN_CONTROL_CONSOLE:
             print("提示: 未安装 pywin32，无法使用显示/隐藏控制台功能。")
        if 'power_monitor' in sys.modules and not power_monitor.PYWIN32_AVAILABLE:
             print("提示: 未安装 pywin32，无法监听电源事件（睡眠/唤醒）。")
        if 'system_actions' in sys.modules and config.KEYBOARD_CANCEL_ENABLED and not system_actions.KEYBOARD_AVAILABLE:
             print("提示: 未安装 keyboard 库或导入失败，无法使用键盘取消功能。")
        # **修改：检查 bark_notifier.BARK_AVAILABLE**
        if 'bark_notifier' in sys.modules and config.BARK_ENABLED and not bark_notifier.BARK_AVAILABLE:
             # 区分是 requests 缺失还是 config 缺失
             if not bark_notifier.REQUESTS_AVAILABLE:
                 print("提示: Bark 推送已启用，但未安装 requests 库 (pip install requests)。")
             elif not bark_notifier.CONFIG_AVAILABLE:
                  print("提示: Bark 推送已启用，但无法导入 config.py。")
             else:
                  print("提示: Bark 推送已启用，但依赖项检查失败。")
        elif 'bark_notifier' in sys.modules and config.BARK_ENABLED and config.BARK_DEVICE_KEY == "YOUR_BARK_KEY":
             print("提示: Bark 推送已启用，但未在 config.py 中配置 BARK_DEVICE_KEY。")


        # 运行主应用逻辑
        main()
        sys.exit(0) # 明确以成功代码退出

    except Exception as e:
        print("\n--- 捕获到未处理的异常 ---", file=sys.stderr)
        traceback.print_exc()
        print("----------------------------------", file=sys.stderr)
        try:
             logging.critical(f"发生未处理的异常: {e}", exc_info=True)
        except NameError:
             print(f"发生未处理的异常 (日志记录不可用): {e}", file=sys.stderr)

        # 尝试显示最终错误通知
        try:
            if 'notifications' in sys.modules and notifications.is_gui_running():
                error_msg = f"发生严重错误，程序即将退出。\n\n错误:\n{traceback.format_exc(limit=5)}"
                notifications.show_custom_notification(f"{config.APP_NAME} 严重错误", error_msg)
                time.sleep(6)
        except Exception as e_notify:
            print(f"(无法显示最终错误通知: {e_notify})", file=sys.stderr)

        input("\n发生错误，按 Enter 键退出...") # 暂停执行
        sys.exit(1) # 以错误代码退出
