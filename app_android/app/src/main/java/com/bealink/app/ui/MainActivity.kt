// 文件路径: app/src/main/java/com/bealink/app/ui/MainActivity.kt
package com.bealink.app.ui // 确保这个 MainActivity 在 ui 包下

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.viewModels
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import com.bealink.app.BealinkApplication // 导入位于 com.bealink.app 包下的 BealinkApplication
import com.bealink.app.ui.screens.DeviceListScreen
import com.bealink.app.ui.theme.BealinkTheme
import com.bealink.app.viewmodel.DeviceViewModel

// 这个 MainActivity 类应该只在这里声明一次
class MainActivity : ComponentActivity() {

    private val deviceViewModel: DeviceViewModel by viewModels {
        DeviceViewModel.DeviceViewModelFactory(
            application, // AndroidViewModel 会自动提供 Application 实例
            // 确保 BealinkApplication 类已正确导入并且 application 可以安全转换为它
            (application as BealinkApplication).deviceRepository
        )
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            BealinkTheme {
                Surface(
                    modifier = Modifier.fillMaxSize(),
                    color = MaterialTheme.colorScheme.background
                ) {
                    val snackbarHostState = remember { SnackbarHostState() }
                    DeviceListScreen(viewModel = deviceViewModel, snackbarHostState = snackbarHostState)
                }
            }
        }
    }
}
