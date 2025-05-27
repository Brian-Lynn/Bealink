// 文件路径: app/src/main/java/com/bealink/app/data/local/DeviceDao.kt
package com.bealink.app.data.local

import androidx.room.*
import kotlinx.coroutines.flow.Flow

@Dao
interface DeviceDao {
    @Query("SELECT * FROM devices ORDER BY name ASC")
    fun getAllDevices(): Flow<List<Device>> // 使用 Flow 实现响应式数据流

    @Query("SELECT * FROM devices WHERE id = :id")
    suspend fun getDeviceById(id: Int): Device?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun insertDevice(device: Device): Long // 返回新插入行的 rowId

    @Update
    suspend fun updateDevice(device: Device)

    @Delete
    suspend fun deleteDevice(device: Device)

    @Query("DELETE FROM devices WHERE id = :id")
    suspend fun deleteDeviceById(id: Int)
}
