import 'dart:io';

import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';
import '../../../core/services/connection_state.dart';

class ClientShellPage extends ConsumerWidget {
  final Widget child;
  const ClientShellPage({super.key, required this.child});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // 响应式连接状态（被踢出/主动退出后立即同步）
    // 客户端模式：关闭“管理请求走回环”开关
    ref.read(apiServiceProvider).serverMode = false;
    final isConnected = ref.watch(connectionStateProvider);
    
    return Scaffold(
      body: Column(
        children: [
          // Top bar
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
            decoration: BoxDecoration(
              color: AppTheme.surfaceColor,
              border: Border(bottom: BorderSide(color: AppTheme.borderColor)),
            ),
            child: Row(
              children: [
                // Connection status indicator (top-left)
                if (isConnected) ...[
                  Container(
                    width: 8,
                    height: 8,
                    decoration: BoxDecoration(
                      color: AppTheme.successColor,
                      shape: BoxShape.circle,
                    ),
                  ),
                  const SizedBox(width: 6),
                  Text(
                    t('已连接'),
                    style: TextStyle(
                      fontSize: 12,
                      color: AppTheme.successColor,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                  const SizedBox(width: 16),
                ],
                Container(
                  width: 32,
                  height: 32,
                  decoration: BoxDecoration(
                    color: const Color(0xFF8B5CF6),
                    borderRadius: BorderRadius.circular(8),
                  ),
                  child: const Icon(Icons.cloud, size: 18, color: Colors.white),
                ),
                const SizedBox(width: 10),
                Text(
                  'FrostLeaves',
                  style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: AppTheme.textPrimary),
                ),
                const Spacer(),
                TextButton.icon(
                  onPressed: () => context.go('/splash'),
                  icon: const Icon(Icons.home_outlined, size: 18),
                  label: Text(t('首页')),
                ),
              ],
            ),
          ),
          // Content
          Expanded(child: child),
          // Bottom nav
          Container(
            decoration: BoxDecoration(
              color: AppTheme.surfaceColor,
              border: Border(top: BorderSide(color: AppTheme.borderColor)),
            ),
            child: SafeArea(
              child: _buildBottomNav(context, isConnected),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildBottomNav(BuildContext context, bool isConnected) {
    final currentPath = GoRouterState.of(context).uri.toString();
    return Row(
      mainAxisAlignment: MainAxisAlignment.spaceAround,
      children: [
        _BottomNavItem(
          icon: isConnected ? Icons.sync_alt : Icons.wifi_tethering_outlined,
          label: isConnected ? t('状态') : t('连接'),
          selected: currentPath.contains('/connect'),
          onTap: () => context.go('/client/connect'),
        ),
        _BottomNavItem(
          icon: Icons.folder_outlined,
          label: t('文件'),
          selected: currentPath.contains('/files'),
          onTap: () => context.go('/client/files'),
        ),
        _BottomNavItem(
          icon: Icons.settings_outlined,
          label: t('设置'),
          selected: currentPath.contains('/settings'),
          onTap: () => context.go('/client/settings'),
        ),
      ],
    );
  }
}

class _BottomNavItem extends StatelessWidget {
  final IconData icon;
  final String label;
  final bool selected;
  final VoidCallback onTap;

  const _BottomNavItem({
    required this.icon,
    required this.label,
    required this.selected,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 10),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 22, color: selected ? AppTheme.primaryColor : AppTheme.textSecondary),
            const SizedBox(height: 4),
            Text(label, style: TextStyle(
              fontSize: 11,
              color: selected ? AppTheme.primaryColor : AppTheme.textSecondary,
              fontWeight: selected ? FontWeight.w600 : FontWeight.normal,
            )),
          ],
        ),
      ),
    );
  }
}
