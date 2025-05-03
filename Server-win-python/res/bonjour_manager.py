# -*- coding: utf-8 -*-

import logging
import socket
from typing import Optional
from zeroconf import ServiceInfo, Zeroconf, IPVersion, ZeroconfServiceTypes, ServiceStateChange

# 导入必要的应用模块
from . import config
from . import utils
from . import notifications # 用于显示错误通知

# --- 模块状态 ---
_zeroconf_instance: Optional[Zeroconf] = None
_service_info: Optional[ServiceInfo] = None
_is_registered = False

# --- 服务注册 ---
def register_service() -> bool:
    """使用 Zeroconf (Bonjour/mDNS) 注册 HTTP 服务"""
    global _zeroconf_instance, _service_info, _is_registered

    if _is_registered:
        logging.warning("Bonjour 服务已经注册。")
        return True

    local_ip = utils.get_local_ip()
    if local_ip == '127.0.0.1':
        logging.warning("未能获取有效的本地网络 IP。服务发现可能会失败。")
        # 无论如何继续，Zeroconf 可能会找到一个接口

    logging.info(f"尝试在端口 {config.SERVER_PORT} 上注册 Bonjour 服务 '{config.INSTANCE_NAME}'")
    logging.info(f"使用的 IP 地址: {local_ip} (Zeroconf 可能会绑定到其他接口)")

    try:
        # 初始化 Zeroconf，为简单起见明确使用 IPv4
        # 让 Zeroconf 自动检测合适的网络接口
        _zeroconf_instance = Zeroconf(ip_version=IPVersion.V4Only)

        # 将 IP 地址字符串转换为 ServiceInfo 所需的打包字节格式
        packed_ip = socket.inet_aton(local_ip)

        # 创建描述服务的 ServiceInfo 对象
        _service_info = ServiceInfo(
            type_=config.SERVICE_TYPE,         # 例如 "_http._tcp.local."
            name=config.INSTANCE_NAME,         # 例如 "MyPC._Bealink._http._tcp.local."
            addresses=[packed_ip],             # 字节格式的 IP 地址列表
            port=config.SERVER_PORT,           # 服务监听的端口
            properties={'version': config.APP_VERSION, 'path': '/'}, # 可选的键值对属性
            server=f"{config.HOSTNAME}.local." # mDNS 中指定服务器主机名的标准方式
        )

        logging.info("正在使用 Zeroconf 注册服务...")
        _zeroconf_instance.register_service(_service_info)
        _is_registered = True
        logging.info(f"Bonjour 服务 '{config.INSTANCE_NAME}' 注册成功。")
        return True

    except OSError as ose:
        # 处理常见的操作系统错误，如端口 5353 被占用或权限问题
        error_message = f"Bonjour 操作系统错误: {ose}"
        detailed_message = f"无法注册服务: {ose}\n"
        if "address already in use" in str(ose).lower() or "only one usage of each socket address" in str(ose).lower():
            error_message = f"Bonjour 注册失败：UDP 端口 5353 可能已被占用或被防火墙阻止。"
            detailed_message += "请检查 Apple Bonjour 服务、iTunes 是否运行，或检查防火墙设置 (允许 UDP 5353)。"
            logging.error(error_message, exc_info=True)
        elif "access denied" in str(ose).lower() or "permission denied" in str(ose).lower():
             error_message = f"Bonjour 注册失败：权限被拒绝。请尝试以管理员身份运行。"
             detailed_message += "尝试以管理员身份运行程序。"
             logging.error(error_message, exc_info=True)
        else:
            logging.error(f"Bonjour 注册失败，发生 OSError: {ose}", exc_info=True)
            detailed_message += "发生未知网络错误。"

        notifications.show_custom_notification(f"{config.APP_NAME} Bonjour 错误", detailed_message)
        # 如果初始化部分失败，清理 zeroconf 实例
        if _zeroconf_instance:
            try:
                _zeroconf_instance.close()
            except Exception as e_close:
                logging.debug(f"注册失败后关闭 Zeroconf 实例时出错: {e_close}")
        _zeroconf_instance = None
        _service_info = None
        _is_registered = False
        return False

    except Exception as e:
        # 捕获注册期间的任何其他意外错误
        logging.error(f"Bonjour 注册期间发生意外错误: {e}", exc_info=True)
        notifications.show_custom_notification(f"{config.APP_NAME} Bonjour 错误", f"注册服务时发生未知错误: {e}\n请确认 Bonjour 服务已正确安装并运行。")
        if _zeroconf_instance:
            try:
                _zeroconf_instance.close()
            except Exception as e_close:
                 logging.debug(f"注册失败后关闭 Zeroconf 实例时出错: {e_close}")
        _zeroconf_instance = None
        _service_info = None
        _is_registered = False
        return False

# --- 服务注销 ---
def unregister_service():
    """注销服务并关闭 Zeroconf 实例"""
    global _zeroconf_instance, _service_info, _is_registered

    if not _is_registered or not _zeroconf_instance or not _service_info:
        logging.debug("Bonjour 服务未注册或已注销。")
        return

    logging.info(f"正在注销 Bonjour 服务 '{config.INSTANCE_NAME}'...")
    try:
        _zeroconf_instance.unregister_service(_service_info)
        logging.info("服务注销成功。")
    except Exception as e:
        # 记录错误但无论如何继续关闭 Zeroconf
        logging.error(f"注销 Bonjour 服务时出错: {e}", exc_info=True)

    try:
        _zeroconf_instance.close()
        logging.info("Zeroconf 实例已关闭。")
    except Exception as e:
        logging.error(f"关闭 Zeroconf 实例时出错: {e}", exc_info=True)

    _zeroconf_instance = None
    _service_info = None
    _is_registered = False

