// 文件路径: app/src/main/java/com/bealink/app/network/WOLUtil.kt
package com.bealink.app.network

import android.util.Log
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress

object WOLUtil {
    private const val TAG = "Bealink_WOLUtil"
    private const val WOL_PORT = 9

    /**
     * 规范化MAC地址：移除所有非十六进制字符并转为大写。
     * 如果结果是12位十六进制字符，则返回；否则返回null。
     */
    fun normalizeMacAddress(macStr: String?): String? {
        if (macStr.isNullOrBlank()) return null
        // 移除非字母数字字符，然后转大写
        val cleanedMac = macStr.replace(Regex("[^a-fA-F0-9]"), "").uppercase()
        return if (cleanedMac.length == 12 && cleanedMac.all { it.isDigit() || it in 'A'..'F' }) {
            cleanedMac
        } else {
            null // 如果清理后长度不对或包含无效字符，则视为无效
        }
    }

    /**
     * 格式化规范后的MAC地址以便显示（例如 XX:XX:XX:XX:XX:XX）。
     * 传入的 normalizedMac 必须是12位无分隔符的十六进制字符串。
     */
    fun formatMacAddress(normalizedMac: String?): String? {
        if (normalizedMac == null || normalizedMac.length != 12) return null
        return normalizedMac.chunked(2).joinToString(":")
    }

    /**
     * 校验MAC地址是否有效 (通常在规范化之后调用)。
     * normalizedMac 应该是12位无分隔符的十六进制字符串。
     */
    fun isValidMacAddress(normalizedMac: String?): Boolean {
        return normalizedMac != null && normalizedMac.length == 12 && normalizedMac.all { it.isDigit() || it in 'A'..'F' }
    }


    suspend fun sendMagicPacket(macAddress: String, broadcastAddress: String = "255.255.255.255"): Boolean {
        val normalizedMac = normalizeMacAddress(macAddress) // 先规范化
        if (!isValidMacAddress(normalizedMac)) { // 使用规范化后的地址进行校验
            Log.e(TAG, "无效或未能规范化的 MAC 地址: '$macAddress' -> '$normalizedMac'")
            return false
        }
        // normalizedMac 此时肯定是12位大写十六进制字符

        return withContext(Dispatchers.IO) {
            try {
                val macBytes = getMacBytes(normalizedMac!!) // 此时 normalizedMac 不为 null
                if (macBytes == null) { // 双重保险，理论上不会到这里
                    Log.e(TAG, "无法从规范化后的 MAC 地址获取字节: $normalizedMac")
                    return@withContext false
                }

                val bytes = ByteArray(6 + 16 * macBytes.size)
                for (i in 0..5) {
                    bytes[i] = 0xff.toByte() // Magic packet prefix
                }
                for (i in 6 until bytes.size step macBytes.size) {
                    System.arraycopy(macBytes, 0, bytes, i, macBytes.size) // Repeat MAC 16 times
                }

                val address = InetAddress.getByName(broadcastAddress)
                DatagramSocket().use { socket -> // 使用 use 块确保 socket 关闭
                    socket.broadcast = true
                    val packet = DatagramPacket(bytes, bytes.size, address, WOL_PORT)
                    socket.send(packet)
                }
                Log.d(TAG, "WOL 魔术包已发送至 MAC: $normalizedMac (原始输入: $macAddress), 广播地址: $broadcastAddress")
                true
            } catch (e: Exception) {
                Log.e(TAG, "发送 WOL 魔术包失败 for MAC $normalizedMac", e)
                false
            }
        }
    }

    // 从12位规范化MAC字符串获取字节数组
    private fun getMacBytes(normalizedMacStr: String): ByteArray? {
        // 此函数假定 normalizedMacStr 已经是12位有效的十六进制字符
        if (normalizedMacStr.length != 12) return null
        val bytes = ByteArray(6)
        try {
            for (i in 0..5) {
                val hexPair = normalizedMacStr.substring(i * 2, i * 2 + 2)
                bytes[i] = hexPair.toInt(16).toByte()
            }
        } catch (e: NumberFormatException) {
            Log.e(TAG, "MAC地址字节转换失败 (应已在normalize阶段处理): $normalizedMacStr", e)
            return null // 理论上不应发生，因为 normalizeMacAddress 已校验
        }
        return bytes
    }
}
