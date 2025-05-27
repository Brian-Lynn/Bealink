// 文件路径: app/src/main/java/com/bealink/app/ui/components/DeviceSettingsDialog.kt
package com.bealink.app.ui.components

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import com.bealink.app.data.local.Device // 假设 Device.kt 在此路径
import com.bealink.app.network.WOLUtil // 假设 WOLUtil.kt 在此路径

@Composable
fun DeviceSettingsDialog(
    showDialog: Boolean,
    onDismissRequest: () -> Unit,
    onSaveDevice: (id: Int?, name: String, hostname: String?, macAddress: String?) -> Unit,
    existingDevice: Device? = null,
    onDeleteDevice: ((Device) -> Unit)? = null,
    showSnackbar: (String) -> Unit
) {
    if (showDialog) {
        // 使用 remember(key) 来确保在 existingDevice 变化时，状态能正确重置
        var deviceNameInput by remember(existingDevice?.id) { mutableStateOf(existingDevice?.name ?: "") }
        var hostnameInput by remember(existingDevice?.id) { mutableStateOf(existingDevice?.hostname ?: "") }
        var macAddressUserInput by remember(existingDevice?.id) { mutableStateOf(WOLUtil.formatMacAddress(existingDevice?.macAddress) ?: "") }

        // 使用 derivedStateOf 优化设备名称的派生逻辑
        val determinedDeviceName by remember(deviceNameInput, hostnameInput, macAddressUserInput) {
            derivedStateOf {
                if (deviceNameInput.isNotBlank()) deviceNameInput
                else if (hostnameInput.isNotBlank()) hostnameInput
                else if (macAddressUserInput.isNotBlank()) {
                    // 尝试规范化并格式化MAC地址作为名称的一部分
                    WOLUtil.formatMacAddress(WOLUtil.normalizeMacAddress(macAddressUserInput)) ?: macAddressUserInput.uppercase()
                }
                else "" // 如果都为空，则为空字符串
            }
        }

        Dialog(onDismissRequest = onDismissRequest) {
            Card(
                shape = MaterialTheme.shapes.large,
                // Modifier.padding(16.dp) // Dialog 通常由其内容 Card 来控制边距
            ) {
                Column(
                    modifier = Modifier
                        .padding(16.dp) // Card 内部的边距
                        .fillMaxWidth(),
                    horizontalAlignment = Alignment.CenterHorizontally
                ) {
                    Text(
                        text = if (existingDevice == null) "添加新设备" else "编辑设备",
                        style = MaterialTheme.typography.titleLarge,
                        modifier = Modifier.padding(bottom = 16.dp)
                    )

                    OutlinedTextField(
                        value = deviceNameInput,
                        onValueChange = { deviceNameInput = it },
                        label = { Text("设备昵称 (可选)") },
                        placeholder = { Text("例如：书房电脑") },
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.Sentences, imeAction = ImeAction.Next),
                        modifier = Modifier.fillMaxWidth()
                    )
                    Spacer(modifier = Modifier.height(8.dp))

                    OutlinedTextField(
                        value = hostnameInput,
                        onValueChange = { hostnameInput = it.trim() }, // 去除前后空格
                        label = { Text("IP / 主机名 (可选)") },
                        placeholder = { Text("例如: my-pc 或 192.168.1.100") },
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Next),
                        modifier = Modifier.fillMaxWidth()
                    )
                    Text(
                        text = "用于 HTTP 控制, 无需填写 '.local'",
                        style = MaterialTheme.typography.labelSmall,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(start = 4.dp, bottom = 8.dp)
                    )

                    OutlinedTextField(
                        value = macAddressUserInput,
                        onValueChange = { macAddressUserInput = it },
                        label = { Text("MAC 地址 (可选)") },
                        // 修改这里的 placeholder
                        placeholder = { Text("例: AA:BB:CC:11:22:33") },
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, imeAction = ImeAction.Done),
                        modifier = Modifier.fillMaxWidth()
                    )
                    Text(
                        text = "用于网络唤醒 (WOL)",
                        style = MaterialTheme.typography.labelSmall,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(start = 4.dp, bottom = 16.dp)
                    )

                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween, // 使删除按钮和右侧按钮组分开
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        if (existingDevice != null && onDeleteDevice != null) {
                            TextButton(
                                onClick = {
                                    onDeleteDevice(existingDevice)
                                    onDismissRequest() // 删除后关闭对话框
                                },
                                colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error)
                            ) {
                                Icon(Icons.Filled.Delete, contentDescription = "删除设备", tint = MaterialTheme.colorScheme.error)
                                Spacer(Modifier.size(ButtonDefaults.IconSpacing))
                                Text("删除")
                            }
                        } else {
                            // 如果没有删除按钮，为了保持右侧按钮的对齐，可以添加一个等权重的 Spacer
                            Spacer(Modifier.weight(1f))
                        }

                        Row { // 用于将取消和保存按钮组合在一起并推向末尾
                            TextButton(onClick = onDismissRequest) { Text("取消") }
                            Spacer(modifier = Modifier.width(8.dp))
                            Button(onClick = {
                                val finalHostname = hostnameInput.takeIf { it.isNotBlank() }
                                val normalizedMac = WOLUtil.normalizeMacAddress(macAddressUserInput)

                                if (finalHostname == null && normalizedMac == null) {
                                    showSnackbar("主机名和 MAC 地址至少填写一个")
                                } else if (macAddressUserInput.isNotBlank() && normalizedMac == null) {
                                    // 提供更具体的错误反馈
                                    showSnackbar("无效的 MAC 地址格式: '${macAddressUserInput}'")
                                } else {
                                    val finalName = determinedDeviceName.ifBlank {
                                        // 如果昵称为空，优先使用主机名，其次是格式化后的MAC地址
                                        finalHostname ?: WOLUtil.formatMacAddress(normalizedMac) ?: "未命名设备"
                                    }
                                    onSaveDevice(
                                        existingDevice?.id,
                                        finalName,
                                        finalHostname,
                                        normalizedMac // 保存规范化后的MAC地址
                                    )
                                    onDismissRequest() // 保存成功后关闭对话框
                                }
                            }) { Text("保存") }
                        }
                    }
                }
            }
        }
    }
}
