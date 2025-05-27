// 文件路径: app/src/main/java/com/bealink/app/data/local/AppDatabase.kt
package com.bealink.app.data.local

import android.content.Context
import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase

@Database(entities = [Device::class], version = 1, exportSchema = false)
abstract class AppDatabase : RoomDatabase() {
    abstract fun deviceDao(): DeviceDao

    companion object {
        @Volatile //确保INSTANCE对所有线程可见
        private var INSTANCE: AppDatabase? = null

        fun getDatabase(context: Context): AppDatabase {
            return INSTANCE ?: synchronized(this) {
                val instance = Room.databaseBuilder(
                    context.applicationContext,
                    AppDatabase::class.java,
                    "bealink_database"
                )
                    // .fallbackToDestructiveMigration() // 如果需要，在 schema 变更时销毁并重建数据库
                    .build()
                INSTANCE = instance
                instance
            }
        }
    }
}
