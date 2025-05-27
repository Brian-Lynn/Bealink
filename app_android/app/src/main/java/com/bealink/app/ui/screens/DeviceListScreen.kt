// 文件路径: app/src/main/java/com/bealink/app/ui/screens/DeviceListScreen.kt
package com.bealink.app.ui.screens

import android.annotation.SuppressLint
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign // 确保导入 TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.bealink.app.data.local.Device // 假设 Device.kt 在此路径
import com.bealink.app.ui.components.DeviceCard
import com.bealink.app.ui.components.DeviceSettingsDialog
import com.bealink.app.viewmodel.DeviceViewModel
import kotlinx.coroutines.launch

@SuppressLint("UnusedMaterial3ScaffoldPaddingParameter") // 如果使用了 paddingValues，可以移除此注解
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DeviceListScreen(
    viewModel: DeviceViewModel,
    snackbarHostState: SnackbarHostState
) {
    val devicesUiState by viewModel.devicesUiStateFlow.collectAsStateWithLifecycle()
    val coroutineScope = rememberCoroutineScope()

    var showDialog by remember { mutableStateOf(false) }
    var deviceToEdit by remember { mutableStateOf<Device?>(null) }

    LaunchedEffect(Unit) {
        viewModel.snackbarMessage.collect { message ->
            snackbarHostState.showSnackbar(
                message = message,
                duration = SnackbarDuration.Short
            )
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Bealink") },
                colors = TopAppBarDefaults.topAppBarColors(
                    // 使用主题的主色调作为 Toolbar 背景
                    containerColor = MaterialTheme.colorScheme.primary,
                    // Toolbar 上的文字和图标颜色
                    titleContentColor = MaterialTheme.colorScheme.onPrimary,
                    actionIconContentColor = MaterialTheme.colorScheme.onPrimary,
                    navigationIconContentColor = MaterialTheme.colorScheme.onPrimary // 如果有导航图标
                ),
                actions = {
                    IconButton(onClick = {
                        coroutineScope.launch {
                            snackbarHostState.showSnackbar("设置功能待开发", duration = SnackbarDuration.Short)
                        }
                    }) {
                        Icon(
                            imageVector = Icons.Filled.Settings,
                            contentDescription = "设置"
                            // tint 会被 actionIconContentColor 覆盖
                        )
                    }
                }
                // Modifier.statusBarsPadding() // 如果TopAppBar内容与状态栏图标重叠，可以添加这个
                // Material 3 的 TopAppBar 应该会自动处理一些内边距
                // WindowInsets.safeDrawing.only(WindowInsetsSides.Top) 也是一种选择
            )
        },
        floatingActionButton = {
            FloatingActionButton(
                onClick = {
                    deviceToEdit = null
                    showDialog = true
                },
                containerColor = MaterialTheme.colorScheme.primary, // FAB 背景色
                contentColor = MaterialTheme.colorScheme.onPrimary    // FAB 图标颜色
            ) {
                Icon(Icons.Filled.Add, contentDescription = "添加设备")
            }
        },
        floatingActionButtonPosition = FabPosition.End,
        snackbarHost = { SnackbarHost(snackbarHostState) }
    ) { paddingValues -> // Scaffold 提供的内边距，包含了系统栏的尺寸
        Box(
            modifier = Modifier
                .fillMaxSize()
                .padding(paddingValues) // 应用 Scaffold 提供的内边距，非常重要！
        ) {
            if (devicesUiState.isEmpty()) {
                Box(
                    modifier = Modifier.fillMaxSize(), // 这个 Box 会被上面的 paddingValues 正确放置
                    contentAlignment = Alignment.Center
                ) {
                    Text(
                        "还没有添加任何设备，\n点击右下角 + 添加一个吧！",
                        modifier = Modifier.padding(16.dp),
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        textAlign = TextAlign.Center
                    )
                }
            } else {
                LazyColumn(
                    modifier = Modifier.fillMaxSize(), // LazyColumn 也会被 paddingValues 正确放置
                    contentPadding = PaddingValues( // 为 LazyColumn 内部内容额外添加间距
                        top = 8.dp,
                        start = 8.dp,
                        end = 8.dp,
                        // 底部间距要考虑 FAB 的高度，72.dp 是一个常见值，可以按需调整
                        // Scaffold 提供的 paddingValues.calculateBottomPadding() 已经包含了系统导航栏的高度
                        // 所以这里我们主要为 FAB 留出空间
                        bottom = 72.dp + 8.dp // 72dp for FAB, 8dp for additional spacing
                    )
                ) {
                    items(items = devicesUiState, key = { it.device.id }) { deviceState ->
                        DeviceCard(
                            deviceUiState = deviceState,
                            onShutdownClick = { viewModel.shutdownDevice(deviceState) },
                            onSyncToPcClick = { viewModel.syncToPcClipboard(deviceState) },
                            onGetFromPcClick = { viewModel.getFromPcClipboard(deviceState) },
                            onMonitorToggleClick = { viewModel.toggleMonitor(deviceState) },
                            onSleepClick = { viewModel.sleepDevice(deviceState) },
                            onWakeClick = { viewModel.wakeDevice(deviceState) },
                            onCardLongClick = {
                                deviceToEdit = deviceState.device
                                showDialog = true
                            },
                            // DeviceCard 内部已有自己的 padding(vertical = 6.dp, horizontal = 8.dp)
                            // 所以这里不需要额外的 horizontal padding
                            modifier = Modifier
                        )
                    }
                }
            }

            if (showDialog) {
                DeviceSettingsDialog(
                    showDialog = showDialog,
                    onDismissRequest = { showDialog = false },
                    onSaveDevice = { id, name, hostname, macAddress ->
                        viewModel.addOrUpdateDevice(id, name, hostname, macAddress)
                        // Dialog 关闭由 onDismissRequest 或按钮点击后的逻辑处理
                    },
                    existingDevice = deviceToEdit,
                    onDeleteDevice = { device ->
                        viewModel.deleteDeviceUi(device)
                    },
                    showSnackbar = { message ->
                        coroutineScope.launch {
                            snackbarHostState.showSnackbar(message, duration = SnackbarDuration.Short)
                        }
                    }
                )
            }
        }
    }
}
