// 文件路径: app/src/main/java/com/bealink/app/viewmodel/DeviceViewModel.kt
package com.bealink.app.viewmodel

import android.app.Application
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.util.Log
import androidx.compose.ui.semantics.text
import androidx.lifecycle.*
import com.bealink.app.data.local.Device
import com.bealink.app.repository.DeviceRepository
import com.bealink.app.network.WOLUtil // 确保导入
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*

data class DeviceUiState(
    val device: Device,
    var resolvedIp: String? = null, // 存储已解析的IP，避免重复解析
    val isOnline: Boolean = false,   // 完全依赖健康检查结果
    val latency: Long? = null,
    val isLoadingAction: Boolean = false // 仅用于用户点击按钮时的加载状态
)

class DeviceViewModel(
    application: Application,
    private val repository: DeviceRepository
) : AndroidViewModel(application) {

    private val TAG = "Bealink_ViewModel"

    private val _devicesUiStateFlow = MutableStateFlow<List<DeviceUiState>>(emptyList())
    val devicesUiStateFlow: StateFlow<List<DeviceUiState>> = _devicesUiStateFlow.asStateFlow()

    private val _snackbarMessage = MutableSharedFlow<String>() // 用于一次性事件
    val snackbarMessage: SharedFlow<String> = _snackbarMessage.asSharedFlow()

    private var healthCheckJob: Job? = null
    private val HEALTH_CHECK_INTERVAL_MS = 3000L // 3秒检查一次

    init {
        Log.d(TAG, "ViewModel 初始化")
        observeAllDevicesFromDb()
        startPeriodicHealthChecks()
    }

    private fun observeAllDevicesFromDb() {
        viewModelScope.launch {
            repository.allDevices
                .distinctUntilChanged()
                .collectLatest { devicesFromDb ->
                    Log.d(TAG, "数据库设备列表更新，数量: ${devicesFromDb.size}")
                    val currentUiStates = _devicesUiStateFlow.value
                    val newUiStates = devicesFromDb.map { dbDevice ->
                        val existingState = currentUiStates.find { it.device.id == dbDevice.id }
                        DeviceUiState(
                            device = dbDevice,
                            resolvedIp = existingState?.resolvedIp, // 保留已解析的IP
                            isOnline = existingState?.isOnline ?: false, // 保留现有状态，待健康检查更新
                            latency = existingState?.latency,
                            isLoadingAction = existingState?.isLoadingAction ?: false
                        )
                    }
                    _devicesUiStateFlow.value = newUiStates.sortedBy { it.device.getDisplayName() }
                    Log.d(TAG, "UI状态列表已更新，数量: ${newUiStates.size}")

                    // 对新加入或主机名有变化且IP未解析的设备，尝试解析IP
                    newUiStates.forEach { state ->
                        if (state.resolvedIp == null && !state.device.hostname.isNullOrBlank() &&
                            !state.device.hostname!!.matches(Regex("\\b(?:[0-9]{1,3}\\.){3}[0-9]{1,3}\\b"))) {
                            Log.d(TAG, "设备 '${state.device.getDisplayName()}' 需要解析IP，主机名: ${state.device.hostname}")
                            resolveIpForDevice(state.device)
                        }
                    }
                }
        }
    }

    private fun resolveIpForDevice(device: Device) {
        if (device.hostname.isNullOrBlank()) return

        viewModelScope.launch(Dispatchers.IO) {
            Log.d(TAG, "尝试为设备 '${device.getDisplayName()}' (${device.hostname}) 解析IP...")
            val ip = repository.resolveHostnameOnce(device.hostname)
            _devicesUiStateFlow.update { currentList ->
                currentList.map {
                    if (it.device.id == device.id) {
                        if (ip != null) {
                            Log.i(TAG, "设备 '${device.getDisplayName()}' 主机名 '${device.hostname}' 解析成功: $ip")
                            it.copy(resolvedIp = ip)
                        } else {
                            Log.w(TAG, "设备 '${device.getDisplayName()}' 主机名 '${device.hostname}' 解析失败")
                            viewModelScope.launch { _snackbarMessage.emit("无法解析主机名: ${device.hostname}") }
                            it
                        }
                    } else it
                }.sortedBy { it.device.getDisplayName() }
            }
        }
    }

    private fun startPeriodicHealthChecks() {
        healthCheckJob?.cancel()
        healthCheckJob = viewModelScope.launch {
            Log.d(TAG, "周期性健康检查已启动，间隔: $HEALTH_CHECK_INTERVAL_MS ms")
            while (isActive) {
                val currentDeviceStates = _devicesUiStateFlow.value
                if (currentDeviceStates.isNotEmpty()) {
                    currentDeviceStates.forEach { deviceState ->
                        val ipToCheck = deviceState.resolvedIp ?: deviceState.device.hostname?.takeIf { it.matches(Regex("\\b(?:[0-9]{1,3}\\.){3}[0-9]{1,3}\\b")) }
                        if (!ipToCheck.isNullOrBlank()) {
                            launch(Dispatchers.IO) {
                                val (isOnline, latency) = repository.getDeviceHealth(deviceState.device, ipToCheck)
                                _devicesUiStateFlow.update { list ->
                                    list.map {
                                        if (it.device.id == deviceState.device.id) {
                                            it.copy(isOnline = isOnline, latency = latency)
                                        } else it
                                    }.sortedBy { it.device.getDisplayName() }
                                }
                            }
                        } else {
                            _devicesUiStateFlow.update { list ->
                                list.map {
                                    if (it.device.id == deviceState.device.id && (it.isOnline || it.latency != null)) {
                                        it.copy(isOnline = false, latency = null)
                                    } else it
                                }.sortedBy { it.device.getDisplayName() }
                            }
                        }
                    }
                }
                delay(HEALTH_CHECK_INTERVAL_MS)
            }
        }
    }

    fun addOrUpdateDevice(id: Int?, name: String, hostname: String?, macAddress: String?) {
        viewModelScope.launch {
            val finalHostname = hostname?.trim()?.takeIf { it.isNotBlank() }
            val normalizedMac = WOLUtil.normalizeMacAddress(macAddress)

            if (finalHostname == null && normalizedMac == null) {
                _snackbarMessage.emit("主机名和 MAC 地址至少填写一个")
                return@launch
            }
            if (macAddress?.isNotBlank() == true && normalizedMac == null) {
                _snackbarMessage.emit("无效的 MAC 地址格式: $macAddress")
                return@launch
            }

            if (finalHostname != null) {
                val existingDeviceWithSameHostname = _devicesUiStateFlow.value.find {
                    it.device.hostname.equals(finalHostname, ignoreCase = true) && (id == null || it.device.id != id)
                }
                if (existingDeviceWithSameHostname != null) {
                    _snackbarMessage.emit("已存在使用主机名 '$finalHostname' 的设备: ${existingDeviceWithSameHostname.device.getDisplayName()}")
                    return@launch
                }
            }

            val deviceName = name.ifBlank { finalHostname ?: WOLUtil.formatMacAddress(normalizedMac) ?: "未命名设备" }
            val deviceToSave = Device(
                id = id ?: 0,
                name = deviceName,
                hostname = finalHostname,
                macAddress = normalizedMac
            )

            try {
                if (id == null) {
                    repository.insertDevice(deviceToSave)
                    _snackbarMessage.emit("设备已添加: ${deviceToSave.getDisplayName()}")
                } else {
                    repository.updateDevice(deviceToSave)
                    _snackbarMessage.emit("设备已更新: ${deviceToSave.getDisplayName()}")
                }
            } catch (e: Exception) {
                Log.e(TAG, "保存设备失败", e)
                _snackbarMessage.emit("保存设备失败: ${e.message}")
            }
        }
    }

    fun deleteDeviceUi(device: Device) {
        viewModelScope.launch {
            try {
                repository.deleteDevice(device)
                _snackbarMessage.emit("设备 ${device.getDisplayName()} 已删除")
            } catch (e: Exception) {
                Log.e(TAG, "删除设备 ${device.getDisplayName()} 失败", e)
                _snackbarMessage.emit("删除失败: ${e.message}")
            }
        }
    }

    // 通用设备HTTP操作方法
    private fun performDeviceHttpAction(
        deviceState: DeviceUiState,
        actionApiCall: suspend (String?) -> Result<String>,
        actionName: String,
        successMessageFormatter: (String) -> String
    ) {
        val ipToUse = deviceState.resolvedIp ?: deviceState.device.hostname?.takeIf { it.matches(Regex("\\b(?:[0-9]{1,3}\\.){3}[0-9]{1,3}\\b")) }

        if (ipToUse.isNullOrBlank()) {
            viewModelScope.launch {
                _snackbarMessage.emit("无法执行 '$actionName'：设备 '${deviceState.device.getDisplayName()}' 无有效IP地址。")
            }
            return
        }
        if (!deviceState.isOnline && actionName !in listOf("唤醒")) {
            viewModelScope.launch {
                _snackbarMessage.emit("设备 '${deviceState.device.getDisplayName()}' 当前离线，无法执行 '$actionName'。")
            }
            return
        }

        viewModelScope.launch {
            _devicesUiStateFlow.update { list ->
                list.map { if (it.device.id == deviceState.device.id) it.copy(isLoadingAction = true) else it }
            }

            val result = actionApiCall(ipToUse)

            _devicesUiStateFlow.update { list ->
                list.map { if (it.device.id == deviceState.device.id) it.copy(isLoadingAction = false) else it }
            }

            result.fold(
                onSuccess = { response ->
                    // MODIFICATION START: Filter prefix for "同步到电脑" action
                    val finalResponseForSnackbar = if (actionName == "同步到电脑") {
                        response.removePrefix("已复制到剪贴板 📋: ").trim()
                    } else {
                        response
                    }
                    _snackbarMessage.emit(successMessageFormatter(finalResponseForSnackbar))
                    // MODIFICATION END
                },
                onFailure = { error -> _snackbarMessage.emit("'$actionName' 操作失败 (${deviceState.device.getDisplayName()}): ${error.message}") }
            )
        }
    }

    fun shutdownDevice(deviceState: DeviceUiState) = performDeviceHttpAction(deviceState, repository::sendShutdownCommand, "关机", successMessageFormatter = { "关机指令已发送: $it" })

    fun syncToPcClipboard(deviceState: DeviceUiState) {
        val clipboard = getApplication<Application>().getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        val clipData = clipboard.primaryClip
        if (clipData != null && clipData.itemCount > 0) {
            val textToSync = clipData.getItemAt(0).text?.toString()
            if (!textToSync.isNullOrBlank()) {
                performDeviceHttpAction(
                    deviceState,
                    { ip -> repository.syncToPcClipboard(ip, textToSync) },
                    "同步到电脑", // This actionName is used in performDeviceHttpAction for filtering
                    successMessageFormatter = { serverResponse -> // serverResponse here is already filtered
                        "已发送\uD83D\uDCCB: ${serverResponse.take(50)}${if(serverResponse.length > 50) "..." else ""}"
                    }
                )
            } else { viewModelScope.launch { _snackbarMessage.emit("手机剪贴板为空") } }
        } else { viewModelScope.launch { _snackbarMessage.emit("手机剪贴板为空") } }
    }

    fun getFromPcClipboard(deviceState: DeviceUiState) = performDeviceHttpAction(deviceState, repository::getFromPcClipboard, "从电脑同步") { response ->
        val clipboard = getApplication<Application>().getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        clipboard.setPrimaryClip(ClipData.newPlainText("BealinkClip", response))
        "已接收\uD83D\uDCCB: ${response.take(50)}${if(response.length > 50) "..." else ""}"
    }

    fun toggleMonitor(deviceState: DeviceUiState) = performDeviceHttpAction(deviceState, repository::sendMonitorToggleCommand, "亮/熄屏", successMessageFormatter = { it })
    fun sleepDevice(deviceState: DeviceUiState) = performDeviceHttpAction(deviceState, repository::sendSleepCommand, "睡眠", successMessageFormatter = { "睡眠指令已发送: $it" })

    fun wakeDevice(deviceState: DeviceUiState) {
        val macAddress = deviceState.device.macAddress
        if (!WOLUtil.isValidMacAddress(macAddress)) {
            viewModelScope.launch { _snackbarMessage.emit("无法唤醒 '${deviceState.device.getDisplayName()}': MAC 地址无效或未配置") }
            return
        }

        val connectivityManager = getApplication<Application>().getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        val activeNetwork = connectivityManager.activeNetwork
        val networkCapabilities = connectivityManager.getNetworkCapabilities(activeNetwork)
        if (networkCapabilities?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) != true) {
            viewModelScope.launch { _snackbarMessage.emit("提示: 您当前未连接到 Wi-Fi 网络，WOL 可能无法工作。") }
        }

        viewModelScope.launch {
            _devicesUiStateFlow.update { list -> list.map { if (it.device.id == deviceState.device.id) it.copy(isLoadingAction = true) else it } }
            val success = repository.wakeDevice(macAddress)
            _devicesUiStateFlow.update { list -> list.map { if (it.device.id == deviceState.device.id) it.copy(isLoadingAction = false) else it } }
            if (success) {
                _snackbarMessage.emit("WOL 魔术包已发送至 ${deviceState.device.getDisplayName()} (${WOLUtil.formatMacAddress(macAddress)})")
            } else {
                _snackbarMessage.emit("发送 WOL 包失败 for ${deviceState.device.getDisplayName()}")
            }
        }
    }

    override fun onCleared() {
        super.onCleared()
        healthCheckJob?.cancel()
        viewModelScope.launch { repository.cleanupResolver() }
        Log.d(TAG, "DeviceViewModel onCleared")
    }

    class DeviceViewModelFactory(private val application: Application, private val repository: DeviceRepository) : ViewModelProvider.Factory {
        override fun <T : ViewModel> create(modelClass: Class<T>): T {
            if (modelClass.isAssignableFrom(DeviceViewModel::class.java)) {
                @Suppress("UNCHECKED_CAST") return DeviceViewModel(application, repository) as T
            }
            throw IllegalArgumentException("Unknown ViewModel class")
        }
    }
}