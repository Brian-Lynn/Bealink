# -*- coding: utf-8 -*-

import logging
import threading
import base64
import os
import time # **新增：导入 time 用于重试间隔**
from urllib.parse import quote, urlencode
from typing import Optional, Tuple

# 尝试导入 requests 库
try:
    import requests
    REQUESTS_AVAILABLE = True
except ImportError:
    logging.warning("未找到 'requests' 库 (pip install requests)。Bark 推送功能将不可用。")
    REQUESTS_AVAILABLE = False

# 尝试导入 cryptography 库 (用于加密)
try:
    from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
    from cryptography.hazmat.primitives import padding
    from cryptography.hazmat.backends import default_backend
    CRYPTOGRAPHY_AVAILABLE = True
except ImportError:
    logging.warning("未找到 'cryptography' 库 (pip install cryptography)。Bark 加密推送功能将不可用。")
    CRYPTOGRAPHY_AVAILABLE = False

# 导入配置
try:
    from . import config
    CONFIG_AVAILABLE = True
except ImportError:
    logging.error("无法导入配置文件 (config.py)。Bark 推送功能将不可用。")
    CONFIG_AVAILABLE = False
    config = None # 设置为 None 以便后续检查

# 定义 BARK_AVAILABLE 常量
BARK_AVAILABLE = REQUESTS_AVAILABLE and CONFIG_AVAILABLE

# --- 加密辅助函数 (代码无变化) ---
def _encrypt_payload(payload_str: str, key: str, iv: str) -> Optional[Tuple[str, str]]:
    """使用 AES-128-CBC 加密 JSON 字符串，返回 Base64 编码的密文和 IV"""
    if not CRYPTOGRAPHY_AVAILABLE:
        logging.error("Cryptography 库不可用，无法执行加密。")
        return None
    try:
        if len(key) != 16 or len(iv) != 16:
            logging.error("Bark 加密密钥 (KEY) 和初始向量 (IV) 必须是 16 个 ASCII 字符。")
            return None
        key_bytes = key.encode('utf-8')
        iv_bytes = iv.encode('utf-8')
        padder = padding.PKCS7(algorithms.AES.block_size).padder()
        padded_data = padder.update(payload_str.encode('utf-8')) + padder.finalize()
        cipher = Cipher(algorithms.AES(key_bytes), modes.CBC(iv_bytes), backend=default_backend())
        encryptor = cipher.encryptor()
        ciphertext_bytes = encryptor.update(padded_data) + encryptor.finalize()
        ciphertext_b64 = base64.b64encode(ciphertext_bytes).decode('utf-8')
        return ciphertext_b64, iv
    except Exception as e:
        logging.error(f"加密 Bark 载荷时出错: {e}", exc_info=True)
        return None

# --- 发送通知函数 ---
def send_bark_notification(
    title: str,
    body: str,
    group: Optional[str] = None,
    icon: Optional[str] = None,
    sound: Optional[str] = None,
    url: Optional[str] = None,
    copy_text: Optional[str] = None,
    auto_copy: bool = False,
    is_archive: bool = False
    ):
    """
    使用后台线程向 Bark 发送推送通知，支持加密、各种参数和重试机制。
    """
    if not BARK_AVAILABLE:
        logging.warning("Bark 依赖项 (requests 或 config) 不可用，无法发送通知。")
        return
    if group is None: group = getattr(config, 'BARK_GROUP_GENERAL', "Bealink")
    if icon is None: icon = getattr(config, 'BARK_ICON_URL', "")
    if sound is None: sound = getattr(config, 'BARK_DEFAULT_SOUND', "glass")
    if not config.BARK_ENABLED:
        logging.debug("Bark 推送未启用，跳过发送。")
        return
    if not config.BARK_DEVICE_KEY or config.BARK_DEVICE_KEY == "YOUR_BARK_KEY":
        logging.warning("Bark 设备密钥 (BARK_DEVICE_KEY) 未在 config.py 中配置，无法发送通知。")
        return

    # --- 构造请求数据 ---
    payload = { "title": title, "body": body }
    if group: payload['group'] = group
    if icon: payload['icon'] = icon
    if sound: payload['sound'] = sound
    if url: payload['url'] = url
    if copy_text: payload['copy'] = copy_text
    if auto_copy: payload['autoCopy'] = "1"
    if is_archive: payload['isArchive'] = "1"

    # --- 准备请求 ---
    target_url = f"{config.BARK_API_SERVER.rstrip('/')}/{config.BARK_DEVICE_KEY}"
    headers = {}
    request_data = None
    request_params = {}

    # --- 处理加密 ---
    if config.BARK_ENCRYPTION_ENABLED:
        # ... (加密逻辑不变) ...
        if not CRYPTOGRAPHY_AVAILABLE:
            logging.error("已启用 Bark 加密，但 Cryptography 库不可用。无法发送加密通知。")
            return
        if not config.BARK_ENCRYPTION_KEY or len(config.BARK_ENCRYPTION_KEY) != 16 or \
           not config.BARK_ENCRYPTION_IV or len(config.BARK_ENCRYPTION_IV) != 16:
            logging.error("已启用 Bark 加密，但未正确配置 16 位的 BARK_ENCRYPTION_KEY 或 BARK_ENCRYPTION_IV。")
            return
        import json
        try:
            payload_json_str = json.dumps(payload, ensure_ascii=False)
        except Exception as e_json:
             logging.error(f"将载荷转换为 JSON 时出错: {e_json}")
             return
        encryption_result = _encrypt_payload(payload_json_str, config.BARK_ENCRYPTION_KEY, config.BARK_ENCRYPTION_IV)
        if encryption_result:
            ciphertext_b64, iv = encryption_result
            headers = {'Content-Type': 'application/x-www-form-urlencoded'}
            request_data = { 'ciphertext': ciphertext_b64, 'iv': iv }
            logging.debug("载荷已加密，将使用 POST 发送。")
        else:
            logging.error("加密载荷失败，取消发送 Bark 通知。")
            return
    else:
        # 不加密，准备 POST JSON 数据
        headers = {'Content-Type': 'application/json; charset=utf-8'}
        import json
        try:
             request_data = json.dumps(payload, ensure_ascii=False).encode('utf-8')
        except Exception as e_json:
             logging.error(f"将载荷转换为 JSON 时出错: {e_json}")
             return
        logging.debug("载荷未加密，将使用 POST 发送 JSON。")

    # --- 定义后台发送任务 (包含重试逻辑) ---
    def send_request():
        log_title = payload.get("title", "无标题")
        log_body = payload.get("body", "无内容")[:50] + '...'
        logging.info(f"准备向 Bark 发送通知: Title='{log_title}', Body='{log_body}'")
        logging.debug(f"目标 URL: {target_url}")

        # **新增：重试逻辑**
        max_retries = 3
        retry_delay = 5 # 重试间隔秒数

        for attempt in range(max_retries):
            logging.debug(f"发送 Bark 通知尝试 #{attempt + 1}/{max_retries}...")
            try:
                # 根据数据类型决定请求方法 (优先 POST)
                if isinstance(request_data, dict) or isinstance(request_data, bytes):
                    response = requests.post(target_url, headers=headers, data=request_data, timeout=config.BARK_TIMEOUT)
                else:
                    logging.warning("准备发送 Bark 请求，但 request_data 类型未知，尝试 GET。")
                    response = requests.get(target_url, headers=headers, params=request_params, timeout=config.BARK_TIMEOUT)

                logging.debug(f"Bark 响应状态码: {response.status_code}")
                response.raise_for_status() # 检查 HTTP 错误 (4xx, 5xx)

                # 检查 Bark 返回的 JSON 状态码
                try:
                    response_json = response.json()
                    logging.debug(f"Bark 响应 JSON: {response_json}")
                    if response_json.get("code") == 200:
                         logging.info(f"Bark 通知发送成功: Title='{log_title}' (尝试 #{attempt + 1})")
                         return # **成功则退出重试循环**
                    else:
                         logging.warning(f"Bark 服务器返回非成功状态 (尝试 #{attempt + 1}): {response_json}")
                         # 对于 Bark 服务器返回的逻辑错误，通常不需要重试
                         return # 直接退出
                except requests.exceptions.JSONDecodeError:
                     logging.warning(f"Bark 响应不是有效的 JSON (尝试 #{attempt + 1}): {response.text}")
                     # 响应格式错误，可能也不需要重试
                     return # 直接退出

            # **修改：只捕获可能由网络问题引起的异常以进行重试**
            except requests.exceptions.Timeout:
                logging.error(f"发送 Bark 通知超时 (尝试 #{attempt + 1}/{max_retries})。 URL: {target_url}")
            except requests.exceptions.SSLError as ssl_err:
                 logging.error(f"发送 Bark 通知时发生 SSL 错误 (尝试 #{attempt + 1}/{max_retries}): {ssl_err}", exc_info=False)
            except requests.exceptions.ConnectionError as conn_err:
                 logging.error(f"发送 Bark 通知时发生连接错误 (尝试 #{attempt + 1}/{max_retries}): {conn_err}", exc_info=False)
            except requests.exceptions.RequestException as req_err:
                 # 捕获其他 requests 相关的错误
                 logging.error(f"发送 Bark 通知时发生请求错误 (尝试 #{attempt + 1}/{max_retries}): {req_err}", exc_info=False)
            except Exception as e:
                # 捕获其他意外错误，不进行重试
                logging.error(f"发送 Bark 通知时发生未知错误 (尝试 #{attempt + 1}): {e}", exc_info=True)
                return # 未知错误，退出重试

            # 如果还没到最后一次尝试，并且发生了可重试的错误，则等待后重试
            if attempt < max_retries - 1:
                logging.info(f"将在 {retry_delay} 秒后重试发送 Bark 通知...")
                time.sleep(retry_delay)
            else:
                logging.error(f"已达到最大重试次数 ({max_retries})，Bark 通知发送失败: Title='{log_title}'")

    # --- 启动后台线程 ---
    try:
        bark_thread = threading.Thread(target=send_request, name="BarkNotifierThread", daemon=True)
        bark_thread.start()
    except Exception as e:
        logging.error(f"启动 Bark 通知线程失败: {e}", exc_info=True)

