import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';
import '../../../core/services/local_storage_service.dart';
import 'client_about_page.dart';
import '../../../core/widgets/theme_mode_selector.dart';
import '../../../core/widgets/language_selector.dart';

class ClientSettingsPage extends ConsumerStatefulWidget {
  const ClientSettingsPage({super.key});
  @override
  ConsumerState<ClientSettingsPage> createState() => _ClientSettingsPageState();
}

class _ClientSettingsPageState extends ConsumerState<ClientSettingsPage> {
  final _serverUrlController = TextEditingController();
  bool _isLoading = true;

  @override
  void initState() {
    super.initState();
    _loadSavedConfig();
  }

  Future<void> _loadSavedConfig() async {
    final savedUrl = await localStorageService.getServerUrl();
    if (savedUrl != null && savedUrl.isNotEmpty) {
      _serverUrlController.text = savedUrl;
    } else {
      _serverUrlController.text = 'https://192.168.1.100:9091';
    }
    setState(() => _isLoading = false);
  }

  @override
  void dispose() { _serverUrlController.dispose(); super.dispose(); }

  @override
  Widget build(BuildContext context) {
    if (_isLoading) {
      return const Center(child: CircularProgressIndicator());
    }

    return SingleChildScrollView(
      padding: const EdgeInsets.all(24),
      child: Center(
        child: SizedBox(width: 480, child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text(t('设置'), style: TextStyle(fontSize: 24, fontWeight: FontWeight.bold, color: AppTheme.textPrimary)),
          const SizedBox(height: 24),

          // Connection settings
          Container(padding: const EdgeInsets.all(20),
            decoration: BoxDecoration(color: AppTheme.cardColor, borderRadius: BorderRadius.circular(12), border: Border.all(color: AppTheme.borderColor)),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(t('连接设置'), style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: AppTheme.textPrimary)),
              const SizedBox(height: 16),
              TextField(controller: _serverUrlController,
                decoration: InputDecoration(labelText: t('服务端地址'), prefixIcon: Icon(Icons.dns_outlined))),
              const SizedBox(height: 12),
              ElevatedButton(onPressed: () {
                final api = ref.read(apiServiceProvider);
                api.setServerUrl(_serverUrlController.text.trim());
                ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t('服务端地址已保存'))));
              }, child: Text(t('保存'))),
            ]),
          ),
          const SizedBox(height: 16),

          // 外观（深色 / 浅色 / 跟随系统，默认深色）
          Container(padding: const EdgeInsets.all(20),
            decoration: BoxDecoration(color: AppTheme.cardColor, borderRadius: BorderRadius.circular(12), border: Border.all(color: AppTheme.borderColor)),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(t('外观'), style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: AppTheme.textPrimary)),
              const SizedBox(height: 12),
              const ThemeModeSelector(),
              const SizedBox(height: 16),
              Text(t('语言 / Language'), style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: AppTheme.textPrimary)),
              const SizedBox(height: 8),
              const LanguageSelector(),
            ]),
          ),
          const SizedBox(height: 16),

          // About（点击进入关于页）
          InkWell(
            borderRadius: BorderRadius.circular(12),
            onTap: () {
              Navigator.of(context).push(
                MaterialPageRoute(builder: (_) => const ClientAboutPage()),
              );
            },
            child: Container(padding: EdgeInsets.all(20),
            decoration: BoxDecoration(color: AppTheme.cardColor, borderRadius: BorderRadius.circular(12), border: Border.all(color: AppTheme.borderColor)),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Row(children: [
                Text(t('关于'), style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: AppTheme.textPrimary)),
                Spacer(),
                Icon(Icons.chevron_right, size: 20, color: AppTheme.textSecondary),
              ]),
              const SizedBox(height: 12),
              Text('Frost Leaves v1.1.0', style: TextStyle(color: AppTheme.textPrimary)),
              const SizedBox(height: 4),
              Text(t('私有组网网盘 · 基于开源组件构建'), style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
              const SizedBox(height: 4),
              Text(t('本软件以 PolyForm Noncommercial 1.0.0 发布（禁止商用）；第三方组件遵循各自开源协议'), style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
              const SizedBox(height: 8),
              Text(t('点此查看运行状态 / 版本信息 / 开发者信息'), style: TextStyle(fontSize: 12, color: AppTheme.primaryColor)),
            ]),
          ),
          ),
        ])),
      ),
    );
  }
}
