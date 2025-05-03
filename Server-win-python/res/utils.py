# -*- coding: utf-8 -*-

import logging
import sys
import socket
import ctypes
import os
import tkinter as tk # 仅用于 safe_destroy 的类型提示

# --- 日志设置 ---
def setup_logging(debug_mode=False):
    """配置日志记录到控制台"""
    log_level = logging.DEBUG if debug_mode else logging.INFO
    log_formatter = logging.Formatter('%(asctime)s - %(levelname)s - %(threadName)s - %(message)s')
    root_logger = logging.getLogger()
    root_logger.setLevel(log_level)

    # 移除现有处理器，避免重复调用时产生重复日志
    if root_logger.hasHandlers():
        root_logger.handlers.clear()

    console_handler = logging.StreamHandler(sys.stdout)
    console_handler.setFormatter(log_formatter)
    root_logger.addHandler(console_handler)
    logging.info("日志系统初始化完成。")
    if debug_mode:
        logging.debug("调试模式已启用。")

# --- 网络工具 ---
def get_local_ip():
    """获取本机的局域网 IP 地址 (优化选择逻辑)"""
    possible_ips = []
    preferred_ips = [] # 存储优先选择的 IP (如 192.168.*)
    fallback_ips = [] # 存储备选 IP

    # 方法一：连接外部服务器
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as s:
            s.settimeout(0.1)
            s.connect(('8.8.8.8', 80))
            ip = s.getsockname()[0]
            if ip and ip != '0.0.0.0':
                possible_ips.append(ip)
            logging.debug(f"通过连接外部服务器找到 IP: {ip}")
    except Exception as e:
        logging.debug(f"通过连接外部服务器查找 IP 失败: {e}")

    # 方法二：获取所有接口地址
    try:
        hostname = socket.gethostname()
        # 获取所有 IPv4 地址信息
        addr_info = socket.getaddrinfo(hostname, None, socket.AF_INET)
        for item in addr_info:
            ip = item[4][0]
            if ip and ip not in possible_ips:
                possible_ips.append(ip)
        logging.debug(f"通过 getaddrinfo 为 {hostname} 找到的 IPs: {possible_ips}")
    except socket.gaierror:
        logging.warning(f"无法解析主机名 '{socket.gethostname()}' 来获取 IP。")
    except Exception as e:
        logging.debug(f"通过 getaddrinfo 查找 IP 失败: {e}")

    # 筛选 IP 地址
    logging.debug(f"所有找到的可能 IP: {possible_ips}")
    for ip in possible_ips:
        if ip.startswith('192.168.') or ip.startswith('10.'):
            # 最优先：常见的家庭/小型办公网络 IP 段
            logging.debug(f"找到优先 IP (192.168.* 或 10.*): {ip}")
            preferred_ips.append(ip)
        elif ip.startswith('172.'):
            # 检查是否在私有范围 172.16.0.0 - 172.31.255.255 内
            try:
                parts = ip.split('.')
                if len(parts) == 4 and 16 <= int(parts[1]) <= 31:
                    # 排除常见的虚拟交换机/Docker IP (例如 172.17.*)，但可以根据需要调整
                    if ip.startswith('172.17.'): # <--- 明确排除 172.17.*
                         logging.debug(f"排除可能是虚拟网卡的 IP: {ip}")
                    else:
                         logging.debug(f"找到可能是私有 IP (172.16-31.*): {ip}")
                         fallback_ips.append(ip) # 作为备选
                else:
                    logging.debug(f"IP {ip} 不在 172.16-31.* 私有范围内。")
            except ValueError:
                logging.debug(f"无法解析 IP 地址部分: {ip}")
        elif not ip.startswith('127.') and not ip.startswith('169.254.') and not ip.startswith('198.18.'):
             # 其他非特殊地址作为最后的备选
             logging.debug(f"找到其他非特殊 IP: {ip}")
             fallback_ips.append(ip)

    # 决定最终使用的 IP
    best_ip = '127.0.0.1' # 默认值
    if preferred_ips:
        best_ip = preferred_ips[0] # 使用第一个找到的优先 IP
        logging.info(f"最终选择：优先的 IP 地址 {best_ip}")
    elif fallback_ips:
        best_ip = fallback_ips[0] # 使用第一个找到的备选 IP
        logging.info(f"最终选择：备选的 IP 地址 {best_ip}")
    else:
        logging.warning(f"未能找到合适的局域网 IP 地址，将使用默认值 {best_ip}。")

    return best_ip

# --- 系统工具 (Windows 特定) ---
def is_admin():
    """检查脚本是否以管理员权限在 Windows 上运行"""
    try:
        is_admin_flag = ctypes.windll.shell32.IsUserAnAdmin() != 0
        logging.debug(f"管理员权限检查: {'是' if is_admin_flag else '否'}")
        return is_admin_flag
    except AttributeError:
        logging.error("无法检查管理员状态：shell32 不可用或缺少 IsUserAnAdmin。")
        return False
    except Exception as e:
        logging.error(f"检查管理员状态时出错: {e}")
        return False

def run_command(command: str) -> bool:
    """使用 os.system 执行系统命令。成功（退出码 0）返回 True，否则返回 False。"""
    logging.info(f"执行命令: {command}")
    try:
        ret_code = os.system(command)
        if ret_code == 0:
            logging.debug(f"命令 '{command}' 执行成功。")
            return True
        else:
            logging.error(f"命令 '{command}' 执行失败，返回码: {ret_code}")
            return False
    except Exception as e:
        logging.error(f"执行命令 '{command}' 时发生异常: {e}", exc_info=True)
        return False

# --- GUI 工具 ---
def safe_destroy(widget: tk.Widget):
    """安全地销毁 Tkinter 组件，如果组件已被销毁则忽略错误。"""
    try:
        if widget and widget.winfo_exists():
            widget.destroy()
            logging.debug(f"组件 {widget} 已安全销毁。")
    except tk.TclError as e:
        if "invalid command name" not in str(e):
            logging.warning(f"安全销毁组件 {widget} 时发生 TclError: {e}")
    except Exception as e:
        logging.warning(f"安全销毁组件 {widget} 时发生未知错误: {e}")
