# -*- coding: utf-8 -*-

import logging
import threading
# **恢复：导入 jsonify**
from flask import Flask, request, jsonify, render_template_string, Response

# 导入必要的应用模块
from . import config
from . import system_actions # 用于触发关机、睡眠、剪贴板操作

# --- Flask 应用初始化 ---
app = Flask(__name__)
# 配置 jsonify 输出 UTF-8 字符
app.config['JSON_AS_ASCII'] = False

# --- Web 服务器路由 ---

@app.route('/')
def index():
    """提供主控制面板 HTML 页面"""
    logging.debug("收到 '/' 路由的请求。")
    # HTML 内容保持不变
    html_content = """
    <!DOCTYPE html>
    <html lang="zh-CN">
    <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <title>{{ app_name }} 控制面板</title>
        <style>
            /* 页面的基本样式 */
            body {
                font-family: 'Microsoft YaHei', 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; /* 优先使用微软雅黑 */
                margin: 0; padding: 20px; background-color: #f0f2f5; color: #333;
                display: flex; flex-direction: column; align-items: center; min-height: 100vh;
            }
            .container {
                background-color: #ffffff; padding: 30px 40px; border-radius: 12px;
                box-shadow: 0 6px 12px rgba(0,0,0,0.1); width: 100%; max-width: 700px;
                text-align: center; margin-bottom: 20px;
            }
            h1 { color: #0056b3; margin-bottom: 25px; font-weight: 600; }
            /* 控制项的网格布局 */
            .control-grid {
                display: grid; grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
                gap: 25px; margin-bottom: 30px;
            }
            .control-item {
                background-color: #f8f9fa; padding: 20px; border-radius: 8px;
                border: 1px solid #dee2e6; display: flex; flex-direction: column;
                align-items: center; text-align: center;
            }
            .control-item h3 { margin-top: 0; margin-bottom: 10px; color: #495057; }
            .control-item p { font-size: 0.9em; color: #6c757d; line-height: 1.5; margin-bottom: 15px; min-height: 40px; }
            /* 按钮样式 */
            button {
                background-color: #007bff; color: white; border: none; padding: 12px 20px;
                border-radius: 6px; font-size: 1em; cursor: pointer;
                transition: background-color 0.2s ease, transform 0.1s ease;
                width: 100%; max-width: 200px; margin-top: auto; font-family: inherit; /* 继承字体 */
            }
            button:hover { background-color: #0056b3; }
            button:active { transform: scale(0.98); }
            button.warning { background-color: #dc3545; }
            button.warning:hover { background-color: #c82333; }
            button.secondary { background-color: #6c757d; }
            button.secondary:hover { background-color: #5a6268; }
            /* 剪贴板特定样式 */
            .clipboard-area { width: 100%; display: flex; flex-direction: column; align-items: center; gap: 10px; }
            input[type="text"] {
                width: 100%; padding: 10px; border: 1px solid #ced4da; border-radius: 6px;
                font-size: 0.95em; box-sizing: border-box; font-family: inherit; /* 继承字体 */
            }
            /* 状态消息区域样式 */
            .status-message { margin-top: 15px; font-weight: bold; min-height: 20px; }
            .status-success { color: #28a745; }
            .status-error { color: #dc3545; }
            .status-info { color: #17a2b8; }
            /* 页脚样式 */
            .footer { margin-top: auto; padding-top: 20px; font-size: 0.9em; color: #6c757d; text-align: center; width: 100%; }
        </style>
    </head>
    <body>
        <div class="container">
            <h1>{{ app_name }} 控制面板</h1>
            <div class="control-grid">
                <div class="control-item">
                    <h3>远程关机</h3>
                    <p>向电脑发送关机指令 (有 {{ delay }} 秒延迟，可在电脑端取消)。</p>
                    <button id="shutdown-button" class="warning">发送关机指令</button>
                </div>
                <div class="control-item">
                    <h3>远程睡眠</h3>
                    <p>向电脑发送睡眠指令 (有 {{ delay }} 秒延迟，可在电脑端取消)。</p>
                    <button id="sleep-button" class="secondary">发送睡眠指令</button>
                </div>
                <div class="control-item">
                    <h3>同步剪贴板 (输入)</h3>
                    <p>将下方输入的文本发送到电脑的剪贴板。</p>
                    <div class="clipboard-area">
                        <input type="text" id="clipboard-input" placeholder="在此输入文本...">
                        <button id="send-input-button">发送到电脑剪贴板</button>
                    </div>
                </div>
                <div class="control-item">
                    <h3>同步剪贴板 (粘贴)</h3>
                    <p>尝试读取您当前设备(手机/平板)的剪贴板内容，并发送到电脑。<br><strong>注意:</strong> 需要浏览器授权，且可能仅在 HTTPS 或 localhost 下有效。</p>
                    <button id="read-client-button">尝试读取并发送</button>
                </div>
            </div>
            <div id="status-message" class="status-message"></div>
        </div>
        <div class="footer">
            <i>{{ app_name }} v{{ version }}</i>
        </div>

        <script>
            // 用于处理按钮点击和 API 调用的 JavaScript
            const statusDiv = document.getElementById('status-message');
            const shutdownButton = document.getElementById('shutdown-button');
            const sleepButton = document.getElementById('sleep-button');
            const sendInputButton = document.getElementById('send-input-button');
            const readClientButton = document.getElementById('read-client-button');
            const clipboardInput = document.getElementById('clipboard-input');

            // --- 显示状态的辅助函数 ---
            function showStatus(message, type = 'info') {
                console.log('[JS] Status Update (' + type + '): ' + message);
                statusDiv.textContent = message;
                statusDiv.className = 'status-message status-' + type;
                if (type !== 'error') {
                    setTimeout(() => {
                        if (statusDiv.textContent === message) {
                             statusDiv.textContent = '';
                             statusDiv.className = 'status-message';
                        }
                    }, 5000);
                }
            }

            // --- 发送通用命令的函数 ---
            function sendCommand(endpoint, actionName) {
                console.log('[JS] Sending command: ' + actionName + ' to ' + endpoint);
                showStatus('正在发送' + actionName + '指令...', 'info');

                fetch(endpoint)
                    .then(response => { // **恢复：处理 JSON 或文本**
                        console.log('[JS] Received response for ' + actionName + ': Status ' + response.status);
                        const contentType = response.headers.get("content-type");
                        if (!response.ok) {
                            // 尝试读取错误文本或 JSON
                            return response.json().catch(() => response.text()).then(errData => {
                                let errorMsg = actionName + '请求失败: ' + response.status + ' ' + response.statusText;
                                if (typeof errData === 'object' && errData !== null && errData.message) {
                                    errorMsg = errData.message; // 优先 JSON 错误
                                } else if (typeof errData === 'string' && errData.length > 0) {
                                    errorMsg = errData; // 备选文本错误
                                }
                                console.error('[JS] Error data for ' + actionName + ':', errData);
                                throw new Error(errorMsg);
                            });
                        }
                        // 尝试解析为 JSON，如果失败则作为文本处理
                        return response.json().catch(() => response.text());
                    })
                    .then(data => { // **恢复：处理 JSON 或文本**
                        console.log('[JS] Success data/text for ' + actionName + ':', data);
                        let messageToShow = actionName + '指令已发送！'; // Default
                        if (typeof data === 'object' && data !== null && data.message) {
                            messageToShow = data.message; // Use JSON message if available
                        } else if (typeof data === 'string') {
                            messageToShow = data; // Use text if it's text
                        }
                        showStatus(messageToShow, 'success');
                    })
                    .catch(error => {
                        console.error('[JS] Error sending ' + actionName + ' command:', error);
                        showStatus('发送' + actionName + '指令失败: ' + error.message, 'error');
                    });
            }

            // --- 发送剪贴板输入内容的函数 ---
            function sendClipboardInput() {
                const text = clipboardInput.value;
                console.log("[JS] Send clipboard input button clicked.");
                if (!text) {
                    showStatus('请输入要发送到剪贴板的文本！', 'error');
                    return;
                }
                const encodedText = encodeURIComponent(text);
                const endpoint = '/clip/' + encodedText;
                console.log('[JS] Sending clipboard input to: ' + endpoint);
                showStatus('正在发送剪贴板内容...', 'info');

                fetch(endpoint)
                    .then(response => { // **恢复：处理 JSON 或文本**
                        console.log('[JS] Received response for clipboard input: Status ' + response.status);
                        if (!response.ok) {
                             return response.json().catch(() => response.text()).then(errData => {
                                 let errorMsg = '剪贴板同步失败: ' + response.status + ' ' + response.statusText;
                                 if (typeof errData === 'object' && errData !== null && errData.message) {
                                     errorMsg = errData.message;
                                 } else if (typeof errData === 'string' && errData.length > 0) {
                                     errorMsg = errData;
                                 }
                                 console.error('[JS] Error data for clipboard input:', errData);
                                 throw new Error(errorMsg);
                             });
                        }
                        return response.json().catch(() => response.text());
                    })
                    .then(data => { // **恢复：处理 JSON 或文本**
                        console.log('[JS] Success data/text for clipboard input:', data);
                        let messageToShow = '剪贴板内容已发送！'; // Default
                        if (typeof data === 'object' && data !== null && data.message) {
                            messageToShow = data.message; // Use JSON message
                        } else if (typeof data === 'string') {
                            messageToShow = data; // Use text message (which now includes content)
                        }
                        showStatus(messageToShow, 'success');
                        clipboardInput.value = '';
                    })
                    .catch(error => {
                        console.error('[JS] Error sending clipboard input:', error);
                        showStatus('发送剪贴板内容失败: ' + error.message, 'error');
                    });
            }

            // --- 读取客户端剪贴板的函数 ---
            function readClientClipboard() {
                console.log("[JS] Read client clipboard button clicked.");
                if (!navigator.clipboard || !navigator.clipboard.readText) {
                    showStatus('您的浏览器不支持或禁止读取剪贴板。', 'error');
                    console.warn("[JS] Clipboard API (readText) not supported or not allowed.");
                    return;
                }
                showStatus('正在请求剪贴板权限...', 'info');
                console.log("[JS] Requesting clipboard read permission...");

                navigator.clipboard.readText()
                    .then(text => {
                        console.log("[JS] Clipboard read permission granted.");
                        if (!text) {
                            showStatus('剪贴板为空或无法读取。', 'info');
                            console.log("[JS] Clipboard is empty or unreadable.");
                            throw new Error('剪贴板为空或无法读取。');
                        }
                        console.log('[JS] Text read from client clipboard:', text.substring(0, 50) + '...');
                        showStatus('读取成功，正在发送...', 'info');
                        const encodedText = encodeURIComponent(text);
                        const endpoint = '/clip/' + encodedText;
                        console.log('[JS] Sending client clipboard content to: ' + endpoint);
                        return fetch(endpoint);
                    })
                    .then(response => { // **恢复：处理 JSON 或文本**
                         console.log('[JS] Received response for sending client clipboard: Status ' + response.status);
                         if (!response.ok) {
                             return response.json().catch(() => response.text()).then(errData => {
                                 let errorMsg = '发送剪贴板内容失败: ' + response.status + ' ' + response.statusText;
                                 if (typeof errData === 'object' && errData !== null && errData.message) {
                                     errorMsg = errData.message;
                                 } else if (typeof errData === 'string' && errData.length > 0) {
                                     errorMsg = errData;
                                 }
                                 console.error('[JS] Error data for sending client clipboard:', errData);
                                 throw new Error(errorMsg);
                             });
                         }
                         return response.json().catch(() => response.text());
                    })
                    .then(data => { // **恢复：处理 JSON 或文本**
                        console.log('[JS] Success data/text for sending client clipboard:', data);
                        let messageToShow = '已将您设备剪贴板的内容发送到电脑！'; // Default
                        if (typeof data === 'object' && data !== null && data.message) {
                            messageToShow = data.message; // Use JSON message
                        } else if (typeof data === 'string') {
                            messageToShow = data; // Use text message
                        }
                        showStatus(messageToShow, 'success');
                    })
                    .catch(err => {
                        console.error('[JS] Error reading/sending client clipboard:', err);
                        let errorMsg = '操作失败: ' + err.message;
                        if (err.name === 'NotAllowedError') {
                            errorMsg = '读取剪贴板失败：需要您的授权。';
                        } else if (err.message && err.message.includes('secure context')) {
                            errorMsg = '读取剪贴板失败：需要安全连接 (HTTPS) 或 localhost。';
                        } else if (err.message && err.message.includes('剪贴板为空')) {
                             errorMsg = '剪贴板为空或无法读取。';
                        }
                        showStatus(errorMsg, 'error');
                    });
            }

            // --- 绑定事件监听器 ---
            if (shutdownButton) {
                shutdownButton.addEventListener('click', () => sendCommand('/shutdown', '关机'));
                console.log("[JS] 关机按钮监听器已附加。");
            } else { console.error("[JS] 未找到关机按钮！"); }
            if (sleepButton) {
                sleepButton.addEventListener('click', () => sendCommand('/sleep', '睡眠'));
                console.log("[JS] 睡眠按钮监听器已附加。");
            } else { console.error("[JS] 未找到睡眠按钮！"); }
            if (sendInputButton) {
                sendInputButton.addEventListener('click', sendClipboardInput);
                console.log("[JS] 发送输入按钮监听器已附加。");
            } else { console.error("[JS] 未找到发送输入按钮！"); }
            if (readClientButton) {
                readClientButton.addEventListener('click', readClientClipboard);
                console.log("[JS] 读取客户端按钮监听器已附加。");
            } else { console.error("[JS] 未找到读取客户端按钮！"); }
            console.log("[JS] 页面脚本已加载并附加监听器（如果找到按钮）。");
        </script>
    </body>
    </html>
    """
    # 使用上下文变量渲染 HTML 模板
    return render_template_string(html_content,
                                  port=config.SERVER_PORT,
                                  app_name=config.APP_NAME,
                                  version=config.APP_VERSION,
                                  delay=config.ACTION_DELAY)

@app.route('/ping', methods=['GET'])
def ping():
    """一个简单的端点，用于检查服务器是否存活"""
    logging.info("收到 ping 请求。")
    # **恢复：返回 JSON**
    return jsonify({"status": "ok", "message": f"Pong! {config.APP_NAME} 服务器在线。"}), 200
    # return f"Pong! {config.APP_NAME} 服务器在线。", 200

@app.route('/shutdown', methods=['GET'])
def handle_shutdown():
    """处理 GET 请求以启动关机序列"""
    logging.info("收到关机请求 (GET /shutdown)。")
    try:
        system_actions.shutdown_computer_request() # 触发操作（在单独线程中运行）
        logging.info("关机请求已成功转发给 system_actions。")
        # **恢复：返回 JSON**
        return jsonify({"status": "accepted", "message": f"关机指令已发送，将在 {config.ACTION_DELAY} 秒后执行（可取消）。"}), 202
        # response_text = f"关机指令已发送，将在 {config.ACTION_DELAY} 秒后执行（可取消）。"
        # return Response(response_text, status=202, mimetype='text/plain; charset=utf-8') # 明确指定编码
    except Exception as e:
        logging.error(f"处理 /shutdown 请求时出错: {e}", exc_info=True)
        # **恢复：返回 JSON 错误**
        return jsonify({"status": "error", "message": "处理关机请求时服务器内部错误。"}), 500
        # return "处理关机请求时服务器内部错误。", 500


@app.route('/sleep', methods=['GET'])
def handle_sleep():
    """处理 GET 请求以启动睡眠序列"""
    logging.info("收到睡眠请求 (GET /sleep)。")
    try:
        system_actions.sleep_computer_request() # 触发操作
        logging.info("睡眠请求已成功转发给 system_actions。")
        # **恢复：返回 JSON**
        return jsonify({"status": "accepted", "message": f"睡眠指令已发送，将在 {config.ACTION_DELAY} 秒后执行（可取消）。"}), 202
        # response_text = f"睡眠指令已发送，将在 {config.ACTION_DELAY} 秒后执行（可取消）。"
        # return Response(response_text, status=202, mimetype='text/plain; charset=utf-8')
    except Exception as e:
        logging.error(f"处理 /sleep 请求时出错: {e}", exc_info=True)
        # **恢复：返回 JSON 错误**
        return jsonify({"status": "error", "message": "处理睡眠请求时服务器内部错误。"}), 500
        # return "处理睡眠请求时服务器内部错误。", 500

@app.route('/clip/<path:text_to_sync>', methods=['GET'])
def handle_clipboard(text_to_sync: str):
    """
    处理 GET 请求以同步剪贴板内容。
    要同步的文本取自 URL 路径。
    Flask 会自动解码 URL 路径。
    """
    logging.info(f"收到剪贴板同步请求 (GET /clip/...)。原始文本: '{text_to_sync[:100]}...' (长度: {len(text_to_sync)})")
    try:
        # 触发剪贴板同步操作（在单独线程中运行）
        success = system_actions.sync_clipboard(text_to_sync)
        if success:
            logging.info("剪贴板同步请求已成功转发给 system_actions。")
            # **修改：返回包含内容的 JSON 消息**
            display_text = text_to_sync if len(text_to_sync) <= 50 else text_to_sync[:47] + "..." # 截断长文本
            return jsonify({"status": "accepted", "message": f"已推送剪贴板 \"{display_text}\""}), 202
            # response_text = f"已推送剪贴板 \"{display_text}\""
            # return Response(response_text, status=202, mimetype='text/plain; charset=utf-8')
        else:
            logging.error("system_actions.sync_clipboard 返回 False (启动线程失败?)。")
            # **恢复：返回 JSON 错误**
            return jsonify({"status": "error", "message": "启动剪贴板同步失败。"}), 500
            # return "启动剪贴板同步失败。", 500
    except Exception as e:
        logging.error(f"处理 /clip 请求时出错: {e}", exc_info=True)
        # **恢复：返回 JSON 错误**
        return jsonify({"status": "error", "message": "处理剪贴板同步请求时服务器内部错误。"}), 500
        # return "处理剪贴板同步请求时服务器内部错误。", 500


# --- Web 服务器执行函数 ---
def run_server(stop_event: threading.Event):
    """使用 Waitress（或 Flask 的开发服务器作为备选）运行 Flask Web 服务器"""
    global app # 确保我们使用的是上面定义的 app 实例
    try:
        from waitress import serve
        logging.info(f"正在 {config.SERVER_HOST}:{config.SERVER_PORT} 上启动 Waitress 服务器...")
        serve(app, host=config.SERVER_HOST, port=config.SERVER_PORT, threads=8)
    except ImportError:
        logging.warning("未找到 Waitress (pip install waitress)。正在使用 Flask 开发服务器 (不推荐用于生产环境)。")
        try:
            app.run(host=config.SERVER_HOST, port=config.SERVER_PORT, debug=config.DEBUG, use_reloader=False)
        except OSError as e_dev:
            if "address already in use" in str(e_dev).lower() or "only one usage of each socket address" in str(e_dev).lower():
                 logging.error(f"端口 {config.SERVER_PORT} 已被占用 (Flask Dev Server)。")
                 from . import notifications
                 notifications.show_custom_notification(f"{config.APP_NAME} 错误", f"端口 {config.SERVER_PORT} 已被占用！")
            else:
                 logging.error(f"启动 Flask 开发服务器失败: {e_dev}", exc_info=True)
                 from . import notifications
                 notifications.show_custom_notification(f"{config.APP_NAME} 错误", f"无法启动网页服务器 (开发模式): {e_dev}")
            stop_event.set()
    except OSError as e_waitress:
         if "address already in use" in str(e_waitress).lower() or "only one usage of each socket address" in str(e_waitress).lower():
             logging.error(f"端口 {config.SERVER_PORT} 已被占用 (Waitress)。")
             from . import notifications
             notifications.show_custom_notification(f"{config.APP_NAME} 错误", f"端口 {config.SERVER_PORT} 已被占用！请关闭使用该端口的其他程序。")
         else:
             logging.error(f"启动 Waitress 服务器失败 (OSError): {e_waitress}", exc_info=True)
             from . import notifications
             notifications.show_custom_notification(f"{config.APP_NAME} 错误", f"无法启动网页服务器 (网络错误): {e_waitress}")
         stop_event.set()
    except Exception as e_generic:
        logging.error(f"Web 服务器遇到意外错误: {e_generic}", exc_info=True)
        from . import notifications
        notifications.show_custom_notification(f"{config.APP_NAME} 严重错误", f"网页服务器意外停止: {e_generic}")
        stop_event.set()
    finally:
        logging.info("Web 服务器线程正在停止。")
        if not stop_event.is_set():
            logging.warning("Web 服务器意外停止。正在设置停止事件。")
            stop_event.set()
            from . import notifications
            if notifications._notification_queue:
                 try: notifications._notification_queue.put(None, block=False)
                 except queue.Full: pass
