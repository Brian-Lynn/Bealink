// 文件路径: app/src/main/java/com/bealink/app/network/MDNSResolver.kt
package com.bealink.app.network

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.net.wifi.WifiManager
import android.util.Log
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import java.net.InetAddress
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

class MDNSResolver(private val context: Context) {
    private val TAG = "Bealink_MDNS_NSD" // 统一TAG前缀
    private val nsdManager: NsdManager? = context.getSystemService(Context.NSD_SERVICE) as? NsdManager
    private var wifiLock: WifiManager.MulticastLock? = null

    // 服务类型，与您服务器端配置一致
    private val SERVICE_TYPE = "_http._tcp." // NsdManager 通常不需要 .local 后缀

    private suspend fun acquireWifiLock() {
        withContext(Dispatchers.IO) { // 明确使用 Dispatchers.IO
            try {
                val wifiManager = context.applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
                wifiLock = wifiManager.createMulticastLock("bealink_nsd_multicast_lock")
                wifiLock?.setReferenceCounted(true)
                wifiLock?.acquire()
                Log.d(TAG, "✅ NSD 多播锁已获取")
            } catch (e: Exception) {
                Log.e(TAG, "获取 NSD 多播锁失败", e)
            }
        }
    }

    private suspend fun releaseWifiLock() {
        withContext(Dispatchers.IO) { // 明确使用 Dispatchers.IO
            try {
                if (wifiLock?.isHeld == true) {
                    wifiLock?.release()
                    Log.d(TAG, "✅ NSD 多播锁已释放")
                }
                wifiLock = null
            } catch (e: Exception) {
                Log.e(TAG, "释放 NSD 多播锁失败", e)
            }
        }
    }

    suspend fun resolveHostname(hostname: String, timeoutMillis: Long = 7000): String? { // 略微增加超时给NSD更多机会
        if (nsdManager == null) {
            Log.e(TAG, "NsdManager 未初始化，无法解析")
            return null
        }
        if (hostname.isBlank()) {
            Log.w(TAG, "主机名为空，无法解析")
            return null
        }

        // NsdManager 通常处理的是服务实例名，不包含 .local
        val targetServiceName = hostname.removeSuffix(".local").removeSuffix(".")
        Log.d(TAG, "🎯 开始使用 NsdManager 解析服务实例名: '$targetServiceName' (类型: '$SERVICE_TYPE')")

        acquireWifiLock()
        var discoveryListenerHolder: NsdManager.DiscoveryListener? = null
        var resolvedIp: String? = null

        try {
            resolvedIp = withTimeoutOrNull(timeoutMillis) {
                suspendCancellableCoroutine<String?> { continuation ->
                    val localResolveListener = object : NsdManager.ResolveListener {
                        override fun onResolveFailed(serviceInfo: NsdServiceInfo?, errorCode: Int) {
                            Log.e(TAG, "❌ NsdManager 解析服务 '${serviceInfo?.serviceName}' 失败, 错误码: $errorCode")
                            if (continuation.isActive) {
                                try { discoveryListenerHolder?.let { nsdManager.stopServiceDiscovery(it) } } catch (e: Exception) { Log.w(TAG, "停止发现时出错(onResolveFailed): ${e.message}") }
                                continuation.resume(null)
                            }
                        }

                        override fun onServiceResolved(serviceInfo: NsdServiceInfo?) {
                            if (serviceInfo != null) {
                                val ipAddressObj: InetAddress? = serviceInfo.host
                                val port = serviceInfo.port // 端口信息，虽然我们这里主要用IP
                                Log.i(TAG, "🎉 NsdManager 成功解析服务: '${serviceInfo.serviceName}', 主机对象: $ipAddressObj, IP: ${ipAddressObj?.hostAddress}, 端口: $port")

                                if (ipAddressObj != null) {
                                    val resolvedIpString = ipAddressObj.hostAddress // 获取IP字符串
                                    if (continuation.isActive) {
                                        try { discoveryListenerHolder?.let { nsdManager.stopServiceDiscovery(it) } } catch (e: Exception) { Log.w(TAG, "停止发现时出错(onServiceResolved): ${e.message}") }
                                        continuation.resume(resolvedIpString)
                                    }
                                } else {
                                    Log.w(TAG, "NsdManager onServiceResolved serviceInfo.host 为 null")
                                    if (continuation.isActive) {
                                        try { discoveryListenerHolder?.let { nsdManager.stopServiceDiscovery(it) } } catch (e: Exception) { Log.w(TAG, "停止发现时出错(host null): ${e.message}") }
                                        continuation.resume(null)
                                    }
                                }
                            } else {
                                Log.w(TAG, "NsdManager onServiceResolved 但 serviceInfo 为 null")
                                if (continuation.isActive) {
                                    try { discoveryListenerHolder?.let { nsdManager.stopServiceDiscovery(it) } } catch (e: Exception) { Log.w(TAG, "停止发现时出错(serviceInfo null): ${e.message}") }
                                    continuation.resume(null)
                                }
                            }
                        }
                    }

                    val localDiscoveryListener = object : NsdManager.DiscoveryListener {
                        override fun onStartDiscoveryFailed(serviceType: String?, errorCode: Int) {
                            Log.e(TAG, "❌ NsdManager 启动服务发现失败 (类型: $serviceType), 错误码: $errorCode")
                            if (continuation.isActive) {
                                try { nsdManager.stopServiceDiscovery(this) } catch (e: Exception) { /* ignore */ }
                                continuation.resumeWithException(RuntimeException("NsdManager onStartDiscoveryFailed: $errorCode"))
                            }
                        }

                        override fun onStopDiscoveryFailed(serviceType: String?, errorCode: Int) {
                            Log.e(TAG, "❌ NsdManager 停止服务发现失败 (类型: $serviceType), 错误码: $errorCode")
                        }

                        override fun onDiscoveryStarted(serviceType: String?) {
                            Log.d(TAG, "✅ NsdManager 服务发现已启动 (类型: $serviceType)")
                        }

                        override fun onDiscoveryStopped(serviceType: String?) {
                            Log.d(TAG, "🛑 NsdManager 服务发现已停止 (类型: $serviceType)")
                        }

                        override fun onServiceFound(serviceInfo: NsdServiceInfo?) {
                            if (serviceInfo != null) {
                                Log.d(TAG, "🔍 NsdManager 发现服务: Name='${serviceInfo.serviceName}', Type='${serviceInfo.serviceType}'")
                                // NsdServiceInfo.serviceName 通常是实例名
                                if (serviceInfo.serviceName?.equals(targetServiceName, ignoreCase = true) == true ||
                                    serviceInfo.serviceName?.startsWith(targetServiceName, ignoreCase = true) == true) { // 有些服务名可能包含额外信息
                                    Log.i(TAG, "✅ NsdManager 发现匹配的服务实例: '${serviceInfo.serviceName}', 开始解析...")
                                    if (continuation.isActive) { // 确保在解析前协程仍然活动
                                        nsdManager.resolveService(serviceInfo, localResolveListener)
                                    } else {
                                        Log.w(TAG, "协程已不活动，不再解析服务 '${serviceInfo.serviceName}'")
                                        try { nsdManager.stopServiceDiscovery(this) } catch (e: Exception) { /* ignore */ }
                                    }
                                } else {
                                    Log.d(TAG, "发现的服务 '${serviceInfo.serviceName}' 与目标 '$targetServiceName' 不匹配，继续查找...")
                                }
                            }
                        }

                        override fun onServiceLost(serviceInfo: NsdServiceInfo?) {
                            Log.w(TAG, "NsdManager 丢失服务: ${serviceInfo?.serviceName}")
                        }
                    }
                    discoveryListenerHolder = localDiscoveryListener

                    continuation.invokeOnCancellation {
                        Log.d(TAG, "NsdManager 解析协程被取消/完成，确保停止服务发现...")
                        try {
                            // discoveryListenerHolder 可能为 null 如果 discoverServices 之前就出错了
                            discoveryListenerHolder?.let { nsdManager.stopServiceDiscovery(it) }
                        } catch (e: IllegalArgumentException) {
                            Log.w(TAG, "尝试停止未注册/已停止的 NsdManager DiscoveryListener: ${e.message}")
                        } catch (e: Exception) {
                            Log.e(TAG, "停止 NsdManager 服务发现时出错 (onCancellation)", e)
                        }
                    }

                    Log.d(TAG, "调用 NsdManager.discoverServices for type: $SERVICE_TYPE")
                    nsdManager.discoverServices(SERVICE_TYPE, NsdManager.PROTOCOL_DNS_SD, discoveryListenerHolder)
                }
            }
        } catch (e: Exception) {
            Log.e(TAG, "NsdManager 解析主机名 '$hostname' 过程中发生最外层异常", e)
            resolvedIp = null // 确保异常时返回 null
        } finally {
            if (discoveryListenerHolder != null && nsdManager != null) {
                try {
                    Log.d(TAG, "解析结束 (finally)，尝试最终停止服务发现")
                    nsdManager.stopServiceDiscovery(discoveryListenerHolder)
                } catch (e: Exception) {
                    // 这里的异常通常是因为监听器已经被注销，可以安全地忽略或只记录警告
                    Log.w(TAG, "在finally块中停止服务发现时出现预期内的异常 (可能已停止): ${e.message}")
                }
            }
            releaseWifiLock() // 释放锁
        }
        return resolvedIp
    }

    // close 方法用于在 ViewModel 清理时调用
    suspend fun close() {
        // NsdManager 不需要显式的 close，但要确保 discoveryListener 被 unregister
        // 这里的 wifiLock 释放是主要的清理工作
        releaseWifiLock()
        Log.d(TAG, "MDNSResolver (NSD) 资源已清理")
    }
}
