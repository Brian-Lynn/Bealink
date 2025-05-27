// 文件路径: app/src/main/java/com/bealink/app/BealinkApplication.kt
package com.bealink.app // 注意：这个文件通常放在根包名下
import kotlin.reflect.KProperty
import android.app.Application
import com.bealink.app.data.local.AppDatabase
import com.bealink.app.repository.DeviceRepository

class BealinkApplication : Application() {

    // 使用 lazy 确保数据库和仓库只在第一次被访问时创建一次
    // 这也保证了它们是单例的

    // 数据库实例
    // "this" 指的是 Application Context
    val database: AppDatabase by lazy { AppDatabase.getDatabase(this) }

    // 设备数据仓库实例
    // 它依赖于上面创建的 database 的 deviceDao()，以及 Application Context
    val deviceRepository: DeviceRepository by lazy { DeviceRepository(database.deviceDao(), this) }

    override fun onCreate() {
        super.onCreate()
        // 这里可以放置一些应用启动时就需要执行的全局初始化代码
        // 比如第三方库的初始化等。
        // 对于 Bealink 目前的需求，主要是通过 lazy 初始化上面的 database 和 repository。
    }
}
