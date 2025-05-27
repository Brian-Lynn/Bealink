// 文件路径: app/src/main/java/com/bealink/app/ui/theme/Theme.kt
package com.bealink.app.ui.theme

import android.app.Activity
import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.core.view.WindowCompat

// 颜色定义 (保持用户原样)
private val LightColors = lightColorScheme(
    primary = YellowPrimary,
    onPrimary = Color.Black, // 黄色背景上的文字用黑色更清晰
    primaryContainer = YellowPrimaryDark,
    onPrimaryContainer = Color.Black,
    secondary = YellowPrimaryDark,
    onSecondary = Color.Black,
    secondaryContainer = Color(0xFFFFF3E0),
    onSecondaryContainer = Color.Black,
    tertiary = GreyUnknown,
    onTertiary = LightGreyOnSurface,
    error = RedOffline,
    onError = Color.White,
    background = LightGreyBackground,
    onBackground = LightGreyOnBackground,
    surface = LightGreySurface,
    onSurface = LightGreyOnSurface,
    surfaceVariant = Color(0xFFEEEEEE),
    onSurfaceVariant = LightGreyOnBackground,
    outline = GreyUnknown
)

private val DarkColors = darkColorScheme(
    primary = YellowPrimary, // 深色模式下主色调可以保持亮黄色
    onPrimary = Color.Black,
    primaryContainer = YellowPrimaryDark,
    onPrimaryContainer = Color.Black,
    secondary = YellowPrimaryDark,
    onSecondary = Color.Black,
    secondaryContainer = Color(0xFF424242),
    onSecondaryContainer = DarkGreyOnSurface,
    tertiary = GreyUnknown,
    onTertiary = DarkGreyOnSurface,
    error = RedOffline,
    onError = Color.Black,
    background = DarkGreyBackground,
    onBackground = DarkGreyOnBackground,
    surface = DarkGreySurface,
    onSurface = DarkGreyOnSurface,
    surfaceVariant = Color(0xFF303030),
    onSurfaceVariant = DarkGreyOnSurface,
    outline = GreyUnknown
)

@Composable
fun BealinkTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    dynamicColor: Boolean = false, // 根据用户需求，保持动态颜色关闭以使用自定义主题
    content: @Composable () -> Unit
) {
    val colorScheme = when {
        dynamicColor && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S -> {
            val context = LocalContext.current
            if (darkTheme) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
        }
        darkTheme -> DarkColors
        else -> LightColors
    }
    val view = LocalView.current
    if (!view.isInEditMode) {
        SideEffect {
            val window = (view.context as Activity).window
            // 1. 设置状态栏背景为透明
            window.statusBarColor = Color.Transparent.toArgb()
            // 2. 让内容布局到状态栏和导航栏后面 (Edge-to-Edge)
            WindowCompat.setDecorFitsSystemWindows(window, false)

            // 3. 根据主题（浅色/深色）设置状态栏前景（图标）颜色
            // isAppearanceLightStatusBars = true 表示状态栏图标为深色 (适用于浅色背景的Toolbar)
            // isAppearanceLightStatusBars = false 表示状态栏图标为浅色 (适用于深色背景的Toolbar)
            // 因为我们的 Toolbar 背景色 YellowPrimary 是亮色，所以状态栏图标应该是深色的，以保证可见性。
            // 如果应用支持深色主题，并且深色主题下的 Toolbar 颜色变深，这里就需要根据 darkTheme 来判断。
            // 在当前设定下，YellowPrimary 始终是主色，且为亮色，onPrimary 是黑色，
            // 因此状态栏图标应为深色。
            WindowCompat.getInsetsController(window, view).isAppearanceLightStatusBars = !darkTheme
            // 如果 Toolbar 背景固定为 YellowPrimary，且希望状态栏图标总是深色，可以设置为:
            // WindowCompat.getInsetsController(window, view).isAppearanceLightStatusBars = true
        }
    }

    MaterialTheme(
        colorScheme = colorScheme,
        typography = AppTypography, // 确保 AppTypography 已定义
        shapes = Shapes, // 确保 Shapes.kt 已定义
        content = content
    )
}
