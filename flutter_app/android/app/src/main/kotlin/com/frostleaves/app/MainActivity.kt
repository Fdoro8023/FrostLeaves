package com.frostleaves.app

import android.os.Build
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

/// 需求 5：向 Flutter 提供设备型号（不引入额外插件，避免联网拉依赖）
class MainActivity : FlutterActivity() {
    private val channelName = "frostleaves/device"

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, channelName)
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "getDeviceModel" -> {
                        val manufacturer = Build.MANUFACTURER ?: ""
                        val model = Build.MODEL ?: ""
                        val release = Build.VERSION.RELEASE ?: ""
                        val label = if (manufacturer.isNotEmpty() &&
                            !model.startsWith(manufacturer, ignoreCase = true)
                        ) {
                            "$manufacturer $model"
                        } else {
                            model
                        }
                        result.success("$label / Android $release")
                    }
                    else -> result.notImplemented()
                }
            }
    }
}