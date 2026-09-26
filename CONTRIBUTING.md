# 贡献指南 / Contributing

感谢你愿意为 Frost Leaves 出一份力！

## 提交 Issue

- 先搜索是否已有同类 issue
- Bug 请附：系统版本（`winver` / Android 版本）、复现步骤、期望结果、实际结果、日志或截图
- 功能建议请说明使用场景与希望解决的问题

## 提交 Pull Request

1. Fork 本仓库，从 `main` 拉出特性分支：`feat/xxx`、`fix/xxx`
2. 保持改动聚焦，一次 PR 只做一件事
3. 提交前自测：
   - 客户端：`cd flutter_app && flutter analyze` 无 error
   - 服务端：`go build ./cmd/server` 通过、`go vet ./...` 无告警
4. 不要提交任何证书、私钥、令牌、`data/`、`config.json`、`local.properties` 等敏感文件
5. PR 描述里写清：改了什么、为什么、怎么验证的

## 代码风格

- Dart：跟随 `flutter analyze` 与 `analysis_options.yaml`
- Go：`gofmt` 格式化，注释用中文或英文均可，保持一致
- 用户可见文案：请同时提供中英文（客户端文案走 `flutter_app/lib/core/i18n/i18n.dart`）

## 商标与合规

请勿在代码、文案、文档中引入第三方商标（例如具体组网客户端品牌名）。
涉及第三方客户端的地方请使用中性描述（「点对点私有组网客户端」），并通过配置项调用。
