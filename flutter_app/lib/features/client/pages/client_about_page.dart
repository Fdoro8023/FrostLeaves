import 'dart:io';
import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';
import '../../../core/services/local_storage_service.dart';
import '../../../core/widgets/floating_message.dart';
import '../../../core/services/update_checker.dart';

/// 客户端【关于】：运行状态 / 版本信息 / 开发者信息（不含敏感项）
class ClientAboutPage extends ConsumerStatefulWidget {
  const ClientAboutPage({super.key});
  @override
  ConsumerState<ClientAboutPage> createState() => _ClientAboutPageState();

  static const String clientVersion = 'v1.1.0 beta';
}

class _ClientAboutPageState extends ConsumerState<ClientAboutPage> {
  bool _loading = true;
  String _serverName = '';
  String _serverVersion = '';
  String _serverUrl = '';
  String _deviceId = '';
  String _status = t('未连接');
  bool _connected = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final api = ref.read(apiServiceProvider);
    final url = await localStorageService.getServerUrl() ?? '';
    final creds = await localStorageService.getCredentials();
    String name = '';
    String ver = '';
    String status = t('未连接');
    bool connected = false;
    if (creds != null) {
      try {
        await api.loadSavedCredentials();
        final resp = await api.getDeviceStatus(creds['deviceId']!);
        final data = resp['data'] as Map<String, dynamic>?;
        name = (data?['server_name'] ?? '').toString();
        ver = (data?['server_version'] ?? '').toString();
        final st = (data?['status'] ?? 'unknown').toString();
        final tv = data?['token_valid'] == true;
        connected = st == 'connected' && tv;
        status = connected ? t('已连接') : (st == 'blacklisted' ? t('已被拉黑') : t('未连接'));
      } catch (_) {
        status = t('无法连接服务端');
      }
    }
    if (!mounted) return;
    setState(() {
      _serverUrl = url;
      _deviceId = creds?['deviceId'] ?? '';
      _serverName = name;
      _serverVersion = ver;
      _status = status;
      _connected = connected;
      _loading = false;
    });
  }

  String get _maskedDeviceId {
    if (_deviceId.isEmpty) return '-';
    return _deviceId.length <= 4 ? _deviceId : '${_deviceId.substring(0, 4)}••••';
  }

  Future<void> _showFeedback() async {
    const email = '2726895865@qq.com';
    await showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t('关于反馈')),
        content: SingleChildScrollView(
          child: Text(
            t('由于开发者团队属于高中生，Bug 修复与版本迭代会很慢，敬请谅解。\n\n'
              '但我们接受您的反馈，请您通过发邮件的方式向我们说明问题，并附带上问题的截图、日志、报错信息等，感谢您对 Frost Leaves 的支持与信任。'),
            style: TextStyle(fontSize: 13, height: 1.6),
          ),
        ),
        actions: [
          TextButton.icon(
            onPressed: () async {
              await Clipboard.setData(const ClipboardData(text: email));
              if (ctx.mounted) Navigator.pop(ctx);
              if (mounted) {
                FloatingMessage.show(context: context, title: t('已复制'), message: email, type: MessageType.success);
              }
            },
            icon: const Icon(Icons.copy, size: 16),
            label: Text(t('复制')),
          ),
          TextButton(onPressed: () => Navigator.pop(ctx), child: Text(t('关闭'))),
        ],
      ),
    );
  }

  Widget _card(String title, List<List<String>> rows) {
    return Container(
      width: double.infinity,
      margin: const EdgeInsets.only(bottom: 16),
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: AppTheme.cardColor,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: AppTheme.borderColor),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(title, style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: AppTheme.textPrimary)),
          const SizedBox(height: 12),
          ...rows.map((r) => Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    SizedBox(
                      width: 110,
                      child: Text(r[0], style: TextStyle(fontSize: 13, color: AppTheme.textSecondary)),
                    ),
                    Expanded(child: SelectableText(r[1], style: TextStyle(fontSize: 13, color: AppTheme.textPrimary))),
                  ],
                ),
              )),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(t('关于 Frost Leaves'))),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : SingleChildScrollView(
              padding: const EdgeInsets.all(24),
              child: Center(
                child: SizedBox(
                  width: 520,
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      _card(t('运行状态'), [
                        [t('连接状态'), _status],
                        [t('服务器名称'), _serverName.isEmpty ? '-' : _serverName],
                        [t('服务器地址'), _serverUrl.isEmpty ? '-' : _serverUrl],
                        [t('本机设备标识'), _maskedDeviceId],
                        [t('组网方式'), t('点对点私有组网（本机已安装的第三方组网客户端）')],
                      ]),
                      _card(t('版本信息'), [
                        [t('客户端版本'), ClientAboutPage.clientVersion],
                        [t('服务端版本'), _connected ? (_serverVersion.isEmpty ? '-' : _serverVersion) : t('未连接')],
                        [t('运行平台'), Platform.isWindows ? 'Windows' : (Platform.isAndroid ? 'Android' : Platform.operatingSystem)],
                        [t('文件传输加密'), t('HTTPS (TLS)，由本软件内置实现')],
                      ]),
                      // 需求 2：检查更新入口
                      Align(
                        alignment: Alignment.centerLeft,
                        child: OutlinedButton.icon(
                          onPressed: () => runUpdateCheck(context, ref.read(apiServiceProvider)),
                          icon: const Icon(Icons.system_update_alt, size: 18),
                          label: Text(t('检查更新')),
                        ),
                      ),
                      const SizedBox(height: 16),
                      Container(
                        width: double.infinity,
                        padding: const EdgeInsets.all(20),
                        decoration: BoxDecoration(
                          color: AppTheme.cardColor,
                          borderRadius: BorderRadius.circular(12),
                          border: Border.all(color: AppTheme.borderColor),
                        ),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(t('开发者信息'), style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: AppTheme.textPrimary)),
                            const SizedBox(height: 12),
                            Text(t('开发者：霜叶  2726895865@qq.com'), style: TextStyle(fontSize: 13, color: AppTheme.textPrimary)),
                            const SizedBox(height: 8),
                            InkWell(
                              onTap: _showFeedback,
                              child: Padding(
                                padding: EdgeInsets.symmetric(vertical: 6),
                                child: Text(t('遇到 bug？联系我们'),
                                    style: TextStyle(fontSize: 14, color: AppTheme.primaryColor, decoration: TextDecoration.underline)),
                              ),
                            ),
                            const SizedBox(height: 8),
                            Text(t('完整用户许可协议存放于程序目录 user_agreement.txt'),
                                style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
                          ],
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),
    );
  }
}
