// 文件路径: app/src/main/java/com/bealink/app/ui/theme/Shapes.kt
package com.bealink.app.ui.theme

import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Shapes
import androidx.compose.ui.unit.dp

val Shapes = Shapes(
    small = RoundedCornerShape(4.dp),
    medium = RoundedCornerShape(8.dp), // 卡片圆角
    large = RoundedCornerShape(16.dp)  // 对话框圆角
)
