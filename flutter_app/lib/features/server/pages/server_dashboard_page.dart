import 'dart:async';

import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';

class ServerDashboardPage extends ConsumerStatefulWidget {
  const ServerDashboardPage({super.key});
  @override
  ConsumerState<ServerDashboardPage> createState() => _ServerDashboardPageState();
}

class _ServerDashboardPageState extends ConsumerState<ServerDashboardPage> {
  Map<String, dynamic>? _status;
  List<Map<String, dynamic>> _recentTasks = [];
  bool _loading = true;
  bool _backendUp = true; // 服务状态检测（需求 4）
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _loadStatus();
    // 每 5 秒检测服务状态（需求 4）
    _timer = Timer.periodic(const Duration(seconds: 5), (_) {
      if (mounted) _loadStatus(silent: true);
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  Future<void> _loadStatus({bool silent = false}) async {
    if (!silent) setState(() => _loading = true);
    try {
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();

      final status = await api.getSystemStatus();
      final statusData = status['data'] as Map<String, dynamic>?;
      _backendUp = true;

      List<Map<String, dynamic>> tasks = [];
      try {
        final auditResp = await api.getAuditLogs();
        final auditData = auditResp['data'] as Map<String, dynamic>?;
        final items = auditData?['items'] as List? ?? [];
        for (var item in items.take(10)) {
          final entry = item as Map<String, dynamic>;
          tasks.add({
            'action': entry['action'] ?? '',
            'device_id': entry['device_id'] ?? '',
            'result': entry['result'] ?? '',
            'detail': entry['detail'] ?? '',
            'created_at': entry['created_at'] ?? '',
            'ip_address': entry['ip_address'] ?? '',
          });
        }
      } catch (_) {}

      if (!mounted) return;
      setState(() {
        _status = statusData;
        _recentTasks = tasks;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _backendUp = false; // 后端未启用 → 红色 Unknown（需求 4）
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(
                t('仪表盘'),
                style: TextStyle(fontSize: 24, fontWeight: FontWeight.bold, color: AppTheme.textPrimary),
              ),
              const Spacer(),
              IconButton(
                icon: const Icon(Icons.refresh),
                onPressed: _loadStatus,
                tooltip: t('刷新'),
              ),
            ],
          ),
          const SizedBox(height: 24),
          // Stats cards（需求 4/5）
          Row(
            children: [
              _StatCard(
                title: t('服务状态'),
                value: _loading ? '...' : (_backendUp ? 'Running' : 'Unknown'),
                icon: _backendUp ? Icons.check_circle : Icons.error,
                color: _backendUp ? AppTheme.successColor : AppTheme.errorColor,
              ),
              const SizedBox(width: 16),
              _StatCard(
                title: t('总设备数'),
                value: '${_loading ? "..." : _status?['devices'] ?? 0}',
                icon: Icons.devices,
                color: const Color(0xFF8B5CF6),
              ),
              const SizedBox(width: 16),
              _StatCard(
                title: t('已连接'),
                value: '${_loading ? "..." : _status?['connected'] ?? 0}',
                icon: Icons.cloud_done,
                color: AppTheme.primaryColor,
                onTap: () => context.go('/server/devices'),
                hint: t('点击进入设备管理'),
              ),
              const SizedBox(width: 16),
              _StatCard(
                title: t('关于我们'),
                value: 'v${_loading ? "..." : _status?['version'] ?? "1.1.0 beta"}',
                icon: Icons.info_outline,
                color: AppTheme.textSecondary,
                onTap: () => context.go('/server/about'),
                hint: t('点击查看系统占用'),
              ),
            ],
          ),
          const SizedBox(height: 32),
          // Recent tasks
          Text(
            t('最近任务'),
            style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: AppTheme.textPrimary),
          ),
          const SizedBox(height: 12),
          if (_loading)
            const Center(child: CircularProgressIndicator())
          else if (_recentTasks.isEmpty)
            Container(
              padding: const EdgeInsets.all(40),
              decoration: BoxDecoration(
                color: AppTheme.cardColor,
                borderRadius: BorderRadius.circular(12),
                border: Border.all(color: AppTheme.borderColor),
              ),
              child: Center(
                child: Column(
                  children: [
                    Icon(Icons.inbox_outlined, size: 48, color: AppTheme.textSecondary),
                    const SizedBox(height: 8),
                    Text(t('暂无任务记录'), style: TextStyle(color: AppTheme.textSecondary)),
                  ],
                ),
              ),
            )
          else
            Container(
              decoration: BoxDecoration(
                color: AppTheme.cardColor,
                borderRadius: BorderRadius.circular(12),
                border: Border.all(color: AppTheme.borderColor),
              ),
              child: Column(
                children: _recentTasks.map((task) => _TaskRow(task: task)).toList(),
              ),
            ),
        ],
      ),
    );
  }
}

class _TaskRow extends StatelessWidget {
  final Map<String, dynamic> task;
  const _TaskRow({required this.task});

  String _formatAction(String action) {
    switch (action) {
      case 'file_upload': return t('上传文件');
      case 'file_download': return t('下载文件');
      case 'file_delete': return t('删除文件');
      case 'file_list': return t('浏览文件');
      case 'file_rename': return t('重命名');
      case 'device_apply': return t('设备申请');
      case 'device_connect': return t('设备连接');
      case 'device_disconnect': return t('设备断开');
      case 'code_generate': return t('生成验证码');
      case 'code_verify': return t('验证设备');
      case 'device_reject': return t('拒绝设备');
      case 'device_blacklist': return t('拉黑设备');
      case 'permission_denied': return t('权限拒绝');
      case 'quota_exceeded': return t('配额超限');
      case 'rate_limited': return t('请求过频');
      case 'upload_blocked': return t('上传被拦截');
      case 'share_create': return t('创建分享');
      case 'share_denied': return t('分享被拒绝');
      default: return action;
    }
  }

  IconData _getActionIcon(String action) {
    switch (action) {
      case 'file_upload': return Icons.upload;
      case 'file_download': return Icons.download;
      case 'file_delete': return Icons.delete;
      case 'file_list': return Icons.folder;
      case 'file_rename': return Icons.edit;
      case 'device_apply': return Icons.device_hub;
      case 'device_connect': return Icons.link;
      case 'device_disconnect': return Icons.link_off;
      case 'code_generate': return Icons.key;
      case 'code_verify': return Icons.verified;
      case 'device_reject': return Icons.block;
      case 'device_blacklist': return Icons.gpp_bad;
      case 'permission_denied': return Icons.lock;
      case 'quota_exceeded': return Icons.warning;
      default: return Icons.info;
    }
  }

  Color _getResultColor(String result) {
    switch (result) {
      case 'success': return AppTheme.successColor;
      case 'denied': return AppTheme.errorColor;
      default: return AppTheme.textSecondary;
    }
  }

  String _formatTime(String timeStr) {
    if (timeStr.isEmpty) return '';
    try {
      final dt = DateTime.parse(timeStr);
      return '${dt.month.toString().padLeft(2, '0')}/${dt.day.toString().padLeft(2, '0')} ${dt.hour.toString().padLeft(2, '0')}:${dt.minute.toString().padLeft(2, '0')}';
    } catch (_) {
      return timeStr;
    }
  }

  @override
  Widget build(BuildContext context) {
    final action = task['action'] as String? ?? '';
    final deviceId = task['device_id'] as String? ?? '';
    final result = task['result'] as String? ?? '';
    final detail = task['detail'] as String? ?? '';
    final createdAt = task['created_at'] as String? ?? '';
    final ipAddress = task['ip_address'] as String? ?? '';

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
      decoration: BoxDecoration(
        border: Border(
          bottom: BorderSide(color: AppTheme.borderColor, width: 0.5),
        ),
      ),
      child: Row(
        children: [
          Icon(_getActionIcon(action), size: 20, color: _getResultColor(result)),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Text(
                      _formatAction(action),
                      style: TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w500,
                        color: AppTheme.textPrimary,
                      ),
                    ),
                    const SizedBox(width: 8),
                    Container(
                      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                      decoration: BoxDecoration(
                        color: _getResultColor(result).withOpacity(0.15),
                        borderRadius: BorderRadius.circular(4),
                      ),
                      child: Text(
                        result == 'success' ? t('完成') : (result == 'denied' ? t(t('拒绝')) : result),
                        style: TextStyle(
                          fontSize: 10,
                          color: _getResultColor(result),
                          fontWeight: FontWeight.w500,
                        ),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 4),
                Row(
                  children: [
                    Text(
                      t('设备: $deviceId'),
                      style: TextStyle(fontSize: 11, color: AppTheme.textSecondary),
                    ),
                    if (ipAddress.isNotEmpty) ...[
                      const SizedBox(width: 12),
                      Text(
                        'IP: $ipAddress',
                        style: TextStyle(fontSize: 11, color: AppTheme.textSecondary),
                      ),
                    ],
                    if (detail.isNotEmpty) ...[
                      const SizedBox(width: 12),
                      Text(
                        detail,
                        style: TextStyle(fontSize: 11, color: AppTheme.textSecondary),
                      ),
                    ],
                  ],
                ),
              ],
            ),
          ),
          const SizedBox(width: 12),
          Text(
            _formatTime(createdAt),
            style: TextStyle(fontSize: 11, color: AppTheme.textSecondary),
          ),
        ],
      ),
    );
  }
}

class _StatCard extends StatelessWidget {
  final String title, value;
  final IconData icon;
  final Color color;
  final VoidCallback? onTap;
  final String? hint;
  const _StatCard({
    required this.title,
    required this.value,
    required this.icon,
    required this.color,
    this.onTap,
    this.hint,
  });

  @override
  Widget build(BuildContext context) {
    return Expanded(
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(12),
        child: Container(
          padding: const EdgeInsets.all(20),
          decoration: BoxDecoration(
            color: AppTheme.cardColor,
            borderRadius: BorderRadius.circular(12),
            border: Border.all(color: AppTheme.borderColor),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(icon, color: color, size: 24),
              const SizedBox(height: 12),
              Text(
                value,
                style: TextStyle(fontSize: 24, fontWeight: FontWeight.bold, color: color),
              ),
              const SizedBox(height: 4),
              Text(
                title,
                style: TextStyle(fontSize: 12, color: AppTheme.textSecondary),
              ),
              if (hint != null) ...[
                const SizedBox(height: 6),
                Text(
                  hint!,
                  style: TextStyle(fontSize: 10, color: color.withOpacity(0.75)),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}
