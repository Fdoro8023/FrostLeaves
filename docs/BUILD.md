# 构建说明 / Build

## 环境要求

| 组件 | 版本 |
| --- | --- |
| Flutter | 3.24.5 stable（Dart SDK ^3.5.4） |
| Go | 1.26.x |
| Visual Studio | 2022，含「使用 C++ 的桌面开发」工作负载（构建 Windows 端） |
| Android SDK | compileSdk 34 / minSdk 21 / NDK 25.1.8937393（构建 APK） |

## 服务端

```bash
go build ./cmd/server         # 生成 FrostLeaves_Server.exe（Windows）
go vet ./...
```

## 客户端

```bash
cd flutter_app
flutter pub get
flutter analyze
flutter build windows --release
flutter build apk --release
```

产物：

- Windows：`flutter_app/build/windows/x64/runner/Release/`（整个目录一起分发）
- Android：`flutter_app/build/app/outputs/flutter-apk/app-release.apk`

## 运行前提

1. 服务端首次启动会自动生成 CA/服务器证书与设备密钥（`data/`）
2. 需要局域网 HTTPS 时，在「系统配置」把 CA 装进本机受信任根证书
3. 需要公网访问时，先自行安装第三方点对点组网客户端并在其后台开启公网访问

## 打包建议

Windows 目录薄但文件多，分发时请整体压缩（包含 `data/` 目录，否则 Flutter 资源缺失）：

```powershell
Compress-Archive -Path .\frost_leaves.exe, .\flutter_windows.dll, .\*.dll, .\data, .\user_agreement.txt `
  -DestinationPath FrostLeaves_Windows.zip -Force
```
