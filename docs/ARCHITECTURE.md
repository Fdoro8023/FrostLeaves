# 架构说明 / Architecture

## 组件

```
┌────────────┐   局域网 HTTPS(9092) / HTTP(9090)   ┌──────────────────────┐
│ Android 端 │ ───────────────────────────────────▶ │  Windows 服务端 (Go)  │
└────────────┘                                      │  · 文件网关 9090      │
┌────────────┐   回环 HTTP(9091=鉴权, 9093=引导)     │  · 鉴权服务 9091      │
│ Windows 桌面│ ───────────────────────────────────▶ │  · 管理界面(内嵌)      │
└────────────┘                                      │  · TLS/CA 9092       │
                                                    │  · 引导端口 9093      │
                                                    └──────────┬───────────┘
                                                               │ 命令行调用
                                                    第三方点对点组网客户端（用户自装）
```

### 服务端（Go，`cmd/server` + `pkg/`）

- `main.go`：服务装配、四个端口监听、管理路由、配置读写
- `admin.go`：管理接口鉴权（回环直通；远程需 `admin_allow_remote` + `X-Admin-Token`）
- `tls.go`：本地 CA 生成、服务器证书签发、CA 安装（Windows certutil）、证书指纹
- `tunnel.go`：公网访问（Funnel）状态/开启/关闭，调用用户自装的组网客户端 CLI
- `qr.go`：二维码 PNG 生成（分享链接供手机扫码）
- `metrics.go` / `netinfo.go`：系统指标与网卡信息（关于页）
- `store.go` / `startup_help_windows.go`：持久化与开机自启
- `pkg/file_gateway`：文件网关（列表、上传、下载、重命名、回收站、配额、审计、分享）
- `pkg/security`：设备令牌签名校验、密钥派生
- `cmd/server/web/index.html`：内嵌 Web 管理页

### 客户端（Flutter，`flutter_app`）

- `lib/core/services/api_service.dart`：HTTP 客户端（鉴权头、管理接口自动回环、错误描述）
- `lib/core/i18n/i18n.dart`：中英文字典与 `t()` 查表（支持含插值的模板匹配）
- `lib/features/server/`：服务端模式（仪表盘、设备管理、文件管理、分享管理、审计、系统配置、关于）
- `lib/features/client/`：客户端模式（连接、文件浏览、设置、关于）
- `lib/features/shared/`：模式选择、首启协议闸门（中英摘要切换）

## 数据流（分享为例）

1. 管理端调用 `POST /api/v1/share/create`（含 `files` 表示多选文件）
2. 网关生成分享码并落盘，返回 `lan_url` / `public_url`
3. 弹窗展示链接，并通过 `GET /api/v1/qr?text=<url>` 拉取二维码 PNG（携带管理令牌、走回环）
4. 访客打开 `/s/<code>`，按权限（只读 / 投递箱）访问文件

## 安全要点

- 管理接口默认仅回环；远程管理必须显式开启并携带管理令牌
- 设备接入需审批，令牌由服务端签发，可拉黑 / 踢出
- 传输加密由内置 HTTPS(TLS) 独立完成，不依赖组网通道
- 首次运行生成 CA 与私钥，仅存本地，不入库
