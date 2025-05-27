// 文件路径: app/src/main/java/com/bealink/app/repository/DeviceRepository.kt
package com.bealink.app.repository

import android.content.Context
import android.util.Log
import com.bealink.app.data.local.Device
import com.bealink.app.data.local.DeviceDao
import com.bealink.app.network.MDNSResolver
import com.bealink.app.network.WOLUtil
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject // 引入 JSONObject
import java.io.IOException
// import java.net.URLEncoder // 不再需要 URLEncoder
import java.util.concurrent.TimeUnit

class DeviceRepository(
    private val deviceDao: DeviceDao,
    private val context: Context
) {
    private val TAG = "Bealink_Repository"

    // 健康检查专用的 OkHttpClient，严格控制超时
    private val healthCheckHttpClient = OkHttpClient.Builder()
        .connectTimeout(500, TimeUnit.MILLISECONDS)
        .readTimeout(2000, TimeUnit.MILLISECONDS)
        .writeTimeout(500, TimeUnit.MILLISECONDS)
        .callTimeout(2800, TimeUnit.MILLISECONDS)
        .build()

    // 其他操作（如剪贴板）可以使用默认超时或稍长超时的 OkHttpClient
    private val defaultHttpClient = OkHttpClient.Builder()
        .connectTimeout(3, TimeUnit.SECONDS)
        .readTimeout(5, TimeUnit.SECONDS)
        .build()

    private val mdnsResolver = MDNSResolver(context)

    val allDevices: Flow<List<Device>> = deviceDao.getAllDevices()

    suspend fun insertDevice(device: Device): Long {
        return deviceDao.insertDevice(device)
    }

    suspend fun updateDevice(device: Device) {
        deviceDao.updateDevice(device)
    }

    suspend fun deleteDevice(device: Device) {
        deviceDao.deleteDevice(device)
    }

    suspend fun resolveHostnameOnce(hostname: String?): String? {
        if (hostname.isNullOrBlank()) return null
        if (hostname.matches(Regex("\\b(?:[0-9]{1,3}\\.){3}[0-9]{1,3}\\b"))) {
            Log.d(TAG, "主机名 '$hostname' 看起来像IP地址，直接使用。")
            return hostname
        }
        Log.d(TAG, "尝试通过 MDNSResolver 解析主机名: '$hostname'")
        return mdnsResolver.resolveHostname(hostname)
    }

    suspend fun getDeviceHealth(device: Device, resolvedIp: String?): Pair<Boolean, Long?> {
        val ipToPing = resolvedIp ?: device.hostname?.takeIf { it.matches(Regex("\\b(?:[0-9]{1,3}\\.){3}[0-9]{1,3}\\b")) }

        if (ipToPing.isNullOrBlank()) {
            return Pair(false, null)
        }
        val url = "http://$ipToPing:8088/ping"
        val request = Request.Builder().url(url).get().build()
        var isOnline = false
        var latency: Long? = null
        try {
            val responseReceived = withTimeoutOrNull(2800L) {
                val startTime = System.currentTimeMillis()
                healthCheckHttpClient.newCall(request).execute().use { response ->
                    latency = System.currentTimeMillis() - startTime
                    isOnline = response.isSuccessful
                    if (!isOnline) {
                        Log.w(TAG, "健康检查失败 (HTTP ${response.code}): ${device.getDisplayName()} ($ipToPing), 耗时: ${latency}ms, 消息: ${response.message}")
                    }
                }
            }
            if (responseReceived == null) {
                Log.w(TAG, "健康检查超时 (外部withTimeoutOrNull): ${device.getDisplayName()} ($ipToPing)")
                isOnline = false
            }
        } catch (e: IOException) {
            isOnline = false
        } catch (e: Exception) {
            Log.e(TAG, "健康检查未知异常: ${device.getDisplayName()} ($ipToPing)", e)
            isOnline = false
        }
        return Pair(isOnline, if (isOnline) latency else null)
    }

    // 为 POST 请求创建一个新的通用方法，或者修改 makeApiCall 以支持 POST
    private suspend fun makePostApiCall(
        resolvedIp: String?,
        endpointPath: String,
        jsonBody: String, // 新增参数：JSON字符串格式的请求体
        client: OkHttpClient = defaultHttpClient
    ): Result<String> {
        if (resolvedIp.isNullOrBlank()) {
            return Result.failure(IllegalArgumentException("API调用IP地址不能为空"))
        }
        return withContext(Dispatchers.IO) {
            try {
                val urlBuilder = "http://$resolvedIp:8088".toHttpUrlOrNull()?.newBuilder()
                    ?: return@withContext Result.failure(IllegalArgumentException("无效的基础URL for IP: $resolvedIp"))
                urlBuilder.addPathSegments(endpointPath.trimStart('/'))
                val url = urlBuilder.build().toString()

                Log.d(TAG, "发起POST API请求: $url, 请求体: $jsonBody")

                // 创建请求体
                val requestBody = jsonBody.toRequestBody("application/json; charset=utf-8".toMediaType())

                // 构建POST请求
                val request = Request.Builder()
                    .url(url)
                    .post(requestBody) // 使用 post 方法并传入请求体
                    .build()

                client.newCall(request).execute().use { response ->
                    val responseBodyString = response.body?.string() ?: ""
                    if (response.isSuccessful) {
                        Log.d(TAG, "POST API请求成功: $url, 响应: $responseBodyString")
                        Result.success(responseBodyString)
                    } else {
                        Log.w(TAG, "POST API请求失败: $url, 状态码: ${response.code}, 响应: $responseBodyString")
                        Result.failure(IOException("服务器错误 (${response.code}): $responseBodyString"))
                    }
                }
            } catch (e: IOException) {
                Log.w(TAG, "POST API网络请求 $endpointPath 失败 for IP $resolvedIp: ${e.message}")
                Result.failure(e)
            } catch (e: IllegalArgumentException) {
                Log.e(TAG, "POST API URL 构建失败 for IP $resolvedIp and $endpointPath", e)
                Result.failure(e)
            } catch (e: Exception) {
                Log.e(TAG, "POST API请求时发生未知错误 for IP $resolvedIp, path $endpointPath", e)
                Result.failure(e)
            }
        }
    }

    // 原来的 GET 请求的通用方法 (如果其他地方还在用，保留它)
    private suspend fun makeGetApiCall(
        resolvedIp: String?,
        endpointPath: String,
        client: OkHttpClient = defaultHttpClient
    ): Result<String> {
        if (resolvedIp.isNullOrBlank()) {
            return Result.failure(IllegalArgumentException("API调用IP地址不能为空"))
        }
        return withContext(Dispatchers.IO) {
            try {
                val urlBuilder = "http://$resolvedIp:8088".toHttpUrlOrNull()?.newBuilder()
                    ?: return@withContext Result.failure(IllegalArgumentException("无效的基础URL for IP: $resolvedIp"))
                urlBuilder.addPathSegments(endpointPath.trimStart('/'))
                val url = urlBuilder.build().toString()
                Log.d(TAG, "发起GET API请求: $url")
                val request = Request.Builder().url(url).get().build() // GET请求

                client.newCall(request).execute().use { response ->
                    val responseBody = response.body?.string() ?: ""
                    if (response.isSuccessful) {
                        Log.d(TAG, "GET API请求成功: $url, 响应: $responseBody")
                        Result.success(responseBody)
                    } else {
                        Log.w(TAG, "GET API请求失败: $url, 状态码: ${response.code}, 响应: $responseBody")
                        Result.failure(IOException("服务器错误 (${response.code}): $responseBody"))
                    }
                }
            } catch (e: IOException) {
                Log.w(TAG, "GET API网络请求 $endpointPath 失败 for IP $resolvedIp: ${e.message}")
                Result.failure(e)
            } catch (e: IllegalArgumentException) {
                Log.e(TAG, "GET API URL 构建失败 for IP $resolvedIp and $endpointPath", e)
                Result.failure(e)
            } catch (e: Exception) {
                Log.e(TAG, "GET API请求时发生未知错误 for IP $resolvedIp, path $endpointPath", e)
                Result.failure(e)
            }
        }
    }


    suspend fun sendSleepCommand(resolvedIp: String?): Result<String> = makeGetApiCall(resolvedIp, "/sleep") // 假设这些还是GET
    suspend fun sendShutdownCommand(resolvedIp: String?): Result<String> = makeGetApiCall(resolvedIp, "/shutdown") // 假设这些还是GET
    suspend fun sendMonitorToggleCommand(resolvedIp: String?): Result<String> = makeGetApiCall(resolvedIp, "/monitor") // 假设这些还是GET

    suspend fun syncToPcClipboard(resolvedIp: String?, content: String): Result<String> {
        // 1. 创建 JSON 对象
        val jsonObject = JSONObject()
        try {
            jsonObject.put("content", content) // key 是 "content"，value 是剪贴板文本
        } catch (e: Exception) {
            Log.e(TAG, "创建剪贴板内容的JSON对象失败", e)
            return Result.failure(IOException("创建JSON失败: ${e.message}"))
        }
        val jsonBodyString = jsonObject.toString()

        // 2. 调用新的 POST 方法
        // 注意：这里的 endpointPath 现在只是 "/clip"，不再包含内容
        return makePostApiCall(resolvedIp, "/clip", jsonBodyString)
    }

    // getFromPcClipboard 仍然是 GET 请求，所以它使用 makeGetApiCall
    suspend fun getFromPcClipboard(resolvedIp: String?): Result<String> = makeGetApiCall(resolvedIp, "/getclip")

    suspend fun wakeDevice(macAddress: String?): Boolean {
        return WOLUtil.sendMagicPacket(macAddress ?: "")
    }

    suspend fun cleanupResolver() {
        mdnsResolver.close()
    }
}