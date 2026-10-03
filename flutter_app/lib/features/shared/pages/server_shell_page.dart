import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';

class ServerShellPage extends ConsumerStatefulWidget {
  final Widget child;
  const ServerShellPage({super.key, required this.child});

  @override
  ConsumerState<ServerShellPage> createState() => _ServerShellPageState();
}

class _ServerShellPageState extends ConsumerState<ServerShellPage> {
  String _serverName = t('服务器');

  @override
  void initState() {
    super.initState();
    // 服务端模式：管理接口强制走回环，避免被 403（admin_allow_remote 默认关闭）
    ref.read(apiServiceProvider).serverMode = true;
    _loadServerName();
  }

  /// 需求 2：读取服务端自定义名称
  Future<void> _loadServerName() async {
    try {
      final api = ref.read(apiServiceProvider);
      final resp = await api.getSystemStatus();
      final data = resp['data'] as Map<String, dynamic>?;
      final name = (data?['server_name'] ?? '').toString();
      if (mounted && name.isNotEmpty) {
        setState(() => _serverName = name);
      }
    } catch (_) {}
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Row(
        children: [
          // Sidebar
          Container(
            width: 220,
            color: AppTheme.surfaceColor,
            child: Column(
              children: [
                const SizedBox(height: 16),
                // Logo
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 16),
                  child: Row(
                    children: [
                      Container(
                        width: 36,
                        height: 36,
                        decoration: BoxDecoration(
                          color: AppTheme.primaryColor,
                          borderRadius: BorderRadius.circular(10),
                        ),
                        child: const Icon(Icons.dns, size: 20, color: Colors.white),
                      ),
                      const SizedBox(width: 10),
                      Text(
                        _serverName,
                        style: TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w700,
                          color: AppTheme.textPrimary,
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(height: 24),
                _NavItem(icon: Icons.dashboard_outlined, label: t('仪表盘'), path: '/server/dashboard'),
                _NavItem(icon: Icons.devices_outlined, label: t('设备管理'), path: '/server/devices'),
                _NavItem(icon: Icons.folder_outlined, label: t('文件管理'), path: '/server/files'),
                _NavItem(icon: Icons.delete_outline, label: t('回收站'), path: '/server/recycle'),
                _NavItem(icon: Icons.admin_panel_settings_outlined, label: t('账号与权限'), path: '/server/accounts'),
                _NavItem(icon: Icons.mark_email_read_outlined, label: t('邮箱注册'), path: '/server/email'),
                _NavItem(icon: Icons.auto_delete_outlined, label: t('数据保留'), path: '/server/retention'),
                _NavItem(icon: Icons.list_alt_outlined, label: t('审计日志'), path: '/server/audit'),
                _NavItem(icon: Icons.share_outlined, label: t('分享管理'), path: '/server/share'),
                _NavItem(icon: Icons.settings_outlined, label: t('系统配置'), path: '/server/settings'),
                const Spacer(),
                _NavItem(icon: Icons.home_outlined, label: t('返回首页'), path: '/splash'),
                const SizedBox(height: 16),
              ],
            ),
          ),
          // Main content
          Expanded(child: widget.child),
        ],
      ),
    );
  }
}

class _NavItem extends StatelessWidget {
  final IconData icon;
  final String label;
  final String path;

  const _NavItem({required this.icon, required this.label, required this.path});

  @override
  Widget build(BuildContext context) {
    final isSelected = GoRouterState.of(context).uri.toString().startsWith(path);
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      child: Material(
        color: isSelected ? AppTheme.primaryColor.withOpacity(0.15) : Colors.transparent,
        borderRadius: BorderRadius.circular(10),
        child: InkWell(
          borderRadius: BorderRadius.circular(10),
          onTap: () => context.go(path),
          child: Container(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
            child: Row(
              children: [
                Icon(icon, size: 20,
                    color: isSelected ? AppTheme.primaryColor : AppTheme.textSecondary),
                const SizedBox(width: 12),
                Text(label, style: TextStyle(
                  fontSize: 14,
                  fontWeight: isSelected ? FontWeight.w600 : FontWeight.normal,
                  color: isSelected ? AppTheme.primaryColor : AppTheme.textSecondary,
                )),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
