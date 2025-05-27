// 文件路径: app/src/main/java/com/bealink/app/ui/components/DeviceCard.kt
package com.bealink.app.ui.components

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.* // 确保导入所有需要的图标
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Paint // 重命名以区分 Compose Paint 和 Android Graphics Paint
import androidx.compose.ui.graphics.drawscope.drawIntoCanvas
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.bealink.app.ui.theme.GreenOnline
import com.bealink.app.ui.theme.RedOffline
import com.bealink.app.viewmodel.DeviceUiState
import com.bealink.app.network.WOLUtil // 导入WOLUtil
import android.graphics.Paint as NativePaint // 用于 setShadowLayer

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun DeviceCard(
    deviceUiState: DeviceUiState,
    onShutdownClick: () -> Unit,
    onSyncToPcClick: () -> Unit,
    onGetFromPcClick: () -> Unit,
    onMonitorToggleClick: () -> Unit,
    onSleepClick: () -> Unit,
    onWakeClick: () -> Unit,
    onCardLongClick: () -> Unit,
    modifier: Modifier = Modifier
) {
    val device = deviceUiState.device
    // 判断设备是否有有效的IP地址（已解析或主机名本身就是IP）以启用HTTP操作
    val hasValidIpForHttp = !deviceUiState.resolvedIp.isNullOrBlank() ||
            device.hostname?.matches(Regex("\\b(?:[0-9]{1,3}\\.){3}[0-9]{1,3}\\b")) == true
    // 判断设备是否有MAC地址以启用唤醒操作
    val canWake = !device.macAddress.isNullOrBlank()

    Card(
        modifier = modifier
            .fillMaxWidth()
            .padding(vertical = 6.dp, horizontal = 8.dp) // 卡片外边距
            .combinedClickable(
                onClick = { /* 短按卡片暂时无操作 */ },
                onLongClick = onCardLongClick // 长按编辑
            ),
        elevation = CardDefaults.cardElevation(defaultElevation = 2.dp), // 卡片阴影
        shape = MaterialTheme.shapes.medium // 卡片圆角
    ) {
        Column(modifier = Modifier.padding(16.dp)) { // 卡片内部内容边距
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.fillMaxWidth()
            ) {
                // 设备昵称
                Text(
                    text = device.getDisplayName(),
                    style = MaterialTheme.typography.headlineSmall,
                    modifier = Modifier.weight(1f) // 占据剩余空间
                )
                Spacer(modifier = Modifier.width(8.dp))

                // 状态指示器 (加载中或在线状态灯)
                Row(verticalAlignment = Alignment.CenterVertically) {
                    if (deviceUiState.isLoadingAction) { // 如果正在执行用户操作
                        CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
                    } else { // 显示在线状态灯
                        val statusColor = if (deviceUiState.isOnline) GreenOnline else RedOffline
                        Box(
                            modifier = Modifier
                                .size(12.dp)
                                .clip(CircleShape)
                                .coloredShadow(color = statusColor, shadowRadius = 3.dp, offsetY = 1.dp) // 带颜色的阴影
                                .background(color = statusColor, shape = CircleShape)
                        )
                    }
                    // 如果在线，显示延迟信息
                    if (deviceUiState.isOnline && deviceUiState.latency != null && !deviceUiState.isLoadingAction) {
                        Text(
                            text = " ${deviceUiState.latency}ms",
                            fontSize = 10.sp,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(start = 4.dp)
                        )
                    }
                }
            }

            // 主机名和已解析的IP（如果不同）
            val displayHostname = device.hostname ?: "N/A"
            val displayIp = deviceUiState.resolvedIp?.let {
                if (it != device.hostname) " ($it)" else ""
            } ?: ""
            Text(
                text = "主机: $displayHostname$displayIp",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )

            // MAC 地址 - 使用 WOLUtil.formatMacAddress 进行格式化显示
            if (!device.macAddress.isNullOrBlank()) {
                Text(
                    text = "MAC: ${WOLUtil.formatMacAddress(device.macAddress)}", // <--- 修改点在这里
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }

            Spacer(modifier = Modifier.height(12.dp))

            // 操作按钮 - 两行，每行三个
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(4.dp), // 按钮间距
                verticalAlignment = Alignment.CenterVertically
            ) {
                DeviceActionButton(icon = Icons.Filled.PowerSettingsNew, text = "关机", onClick = onShutdownClick, enabled = hasValidIpForHttp && deviceUiState.isOnline)
                DeviceActionButton(icon = Icons.Filled.UploadFile, text = "上传剪贴板", onClick = onSyncToPcClick, enabled = hasValidIpForHttp && deviceUiState.isOnline)
                DeviceActionButton(icon = Icons.Filled.DownloadDone, text = "下载剪贴板", onClick = onGetFromPcClick, enabled = hasValidIpForHttp && deviceUiState.isOnline)
            }
            Spacer(modifier = Modifier.height(8.dp)) // 两行按钮之间的间距
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(4.dp),
                verticalAlignment = Alignment.CenterVertically
            ) {
                DeviceActionButton(icon = Icons.Filled.Tonality, text = "亮/熄屏", onClick = onMonitorToggleClick, enabled = hasValidIpForHttp && deviceUiState.isOnline)
                DeviceActionButton(icon = Icons.Filled.Bedtime, text = "睡眠", onClick = onSleepClick, enabled = hasValidIpForHttp && deviceUiState.isOnline)
                DeviceActionButton(icon = Icons.Filled.SettingsEthernet, text = "唤醒", onClick = onWakeClick, enabled = canWake)
            }
        }
    }
}

@Composable
fun RowScope.DeviceActionButton(
    icon: ImageVector,
    text: String,
    onClick: () -> Unit,
    enabled: Boolean = true
) {
    Button(
        onClick = onClick,
        enabled = enabled,
        modifier = Modifier.weight(1f), // 使按钮在行内均分宽度
        contentPadding = PaddingValues(horizontal = 4.dp, vertical = 8.dp), // 按钮内边距
        shape = MaterialTheme.shapes.small // 按钮圆角
    ) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) { // 图标和文字垂直排列
            Icon(icon, contentDescription = text, modifier = Modifier.size(20.dp))
            Spacer(modifier = Modifier.height(2.dp))
            Text(
                text = text,
                fontSize = 9.sp, // 按钮文字略小
                fontWeight = FontWeight.Medium,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis // 超长时省略
            )
        }
    }
}

// 自定义 Modifier 函数，为Composable添加带颜色的阴影效果 (用于状态灯)
fun Modifier.coloredShadow(
    color: Color,
    alpha: Float = 0.4f, // 阴影透明度
    borderRadius: Dp = 0.dp, // 圆角，这里画圆所以不直接用
    shadowRadius: Dp = 4.dp, // 阴影模糊半径a
    offsetY: Dp = 2.dp, // Y轴偏移
    offsetX: Dp = 0.dp  // X轴偏移
): Modifier = this.drawBehind {
    val nativePaint = NativePaint().apply { // 使用 Android 原生 Paint 实现阴影
        isAntiAlias = true
        isDither = true
        style = NativePaint.Style.FILL
    }
    val shadowColorArgb = color.copy(alpha = alpha).toArgb() // 阴影颜色
    val transparentArgb = Color.Transparent.toArgb() // 用于绘制被投影形状的透明色

    this.drawIntoCanvas { canvas ->
        nativePaint.color = transparentArgb // 设置画笔为透明，我们只关心阴影
        nativePaint.setShadowLayer(
            shadowRadius.toPx(),
            offsetX.toPx(),
            offsetY.toPx(),
            shadowColorArgb
        )
        // 绘制一个圆形，阴影会基于这个圆形产生
        canvas.drawCircle(
            center = this.center,
            radius = this.size.minDimension / 2, // 圆的半径
            paint = Paint().apply { this.asFrameworkPaint().set(nativePaint) } // 将原生Paint转为Compose Paint
        )
    }
}
