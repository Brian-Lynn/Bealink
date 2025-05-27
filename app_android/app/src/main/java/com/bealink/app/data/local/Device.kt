// 文件路径: app/src/main/java/com/bealink/app/data/local/Device.kt
package com.bealink.app.data.local

import androidx.room.Entity
import androidx.room.PrimaryKey

@Entity(tableName = "devices")
data class Device(
    @PrimaryKey(autoGenerate = true)
    val id: Int = 0,
    var name: String, // 用户自定义昵称
    var hostname: String?, // 例如 my-pc (不含 .local)，或者 IP 地址
    var macAddress: String? // MAC 地址，用于 WOL
) {
    // 辅助函数，用于显示设备名称
    fun getDisplayName(): String {
        return if (name.isNotBlank()) {
            name
        } else if (!hostname.isNullOrBlank()) {
            hostname!!
        } else if (!macAddress.isNullOrBlank()) {
            macAddress!!
        } else {
            "未知设备" // Fallback
        }
    }
}
