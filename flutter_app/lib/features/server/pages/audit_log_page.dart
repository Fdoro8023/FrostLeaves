import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';
import '../../../core/models/models.dart';

class AuditLogPage extends ConsumerStatefulWidget {
  const AuditLogPage({super.key});
  @override
  ConsumerState<AuditLogPage> createState() => _AuditLogPageState();
}

class _AuditLogPageState extends ConsumerState<AuditLogPage> {
  List<AuditLogEntry> _logs = [];
  bool _loading = true;

  @override
  void initState() { super.initState(); _loadLogs(); }

  Future<void> _loadLogs() async {
    setState(() => _loading = true);
    try {
      final api = ref.read(apiServiceProvider);
      final resp = await api.getAuditLogs();
      final data = resp['data'] as Map<String, dynamic>;
      final items = (data['items'] as List?)?.map((e) => AuditLogEntry.fromJson(e as Map<String, dynamic>)).toList() ?? [];
      setState(() { _logs = items; _loading = false; });
    } catch (e) { setState(() => _loading = false); }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(padding: const EdgeInsets.all(24), child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Text(t(t('审计日志')), style: TextStyle(fontSize: 24, fontWeight: FontWeight.bold, color: AppTheme.textPrimary)),
      const SizedBox(height: 16),
      Expanded(child: _loading ? const Center(child: CircularProgressIndicator())
        : _logs.isEmpty ? Center(child: Text(t(t('暂无日志')), style: TextStyle(color: AppTheme.textSecondary)))
        : ListView.builder(itemCount: _logs.length, itemBuilder: (ctx, i) {
            final log = _logs[i];
            return Card(margin: const EdgeInsets.only(bottom: 6), child: ListTile(
              leading: Icon(_actionIcon(log.action), size: 20, color: _actionColor(log.result)),
              title: Text('${_actionLabel(log.action)} · ${log.deviceId}', style: TextStyle(fontSize: 13, color: AppTheme.textPrimary)),
              subtitle: Text('${log.resourcePath ?? ""} · ${log.createdAt}', style: TextStyle(fontSize: 11, color: AppTheme.textSecondary)),
              trailing: _ResultBadge(result: log.result),
            ));
          }),
      ),
    ]));
  }

  IconData _actionIcon(String action) {
    switch (action) {
      case 'file_upload': return Icons.upload;
      case 'file_download': return Icons.download;
      case 'file_delete': return Icons.delete;
      case 'permission_denied': return Icons.block;
      case 'quota_exceeded': return Icons.warning;
      case 'device_connect': return Icons.cloud_done;
      default: return Icons.info;
    }
  }

  String _actionLabel(String action) {
    switch (action) {
      case 'file_upload': return t('上传文件');
      case 'file_download': return t('下载文件');
      case 'file_delete': return t('删除文件');
      case 'file_rename': return t('重命名文件');
      case 'file_list': return t('文件列表');
      case 'file_recycle': return t('移入回收站');
      case 'file_restore': return t('还原文件');
      case 'dir_create': return t('新建目录');
      case 'recycle_permanent_delete': return t('永久删除');
      case 'recycle_clear': return t('清空回收站');
      case 'upload_blocked': return t('上传被拦截');
      case 'quota_exceeded': return t('超出配额');
      case 'permission_denied': return t('权限拒绝');
      case 'rate_limited': return t('触发限流');
      case 'share_access': return t('访问分享');
      case 'share_create': return t('创建分享');
      case 'share_revoke': return t('撤销分享');
      case 'device_apply': return t('设备申请');
      case 'device_connect': return t('设备接入');
      case 'device_disconnect': return t('踢出设备');
      case 'device_release': return t('设备退出');
      case 'device_force_logout': return t('强制下线');
      case 'device_reject': return t('设备被拒');
      case 'device_blacklist': return t('拉黑设备');
      case 'device_unblock': return t('解除拉黑');
      case 'device_data_purged': return t('设备数据清除');
      case 'code_generate': return t('生成验证码');
      case 'code_verify': return t('校验验证码');
      case 'code_expire': return t('验证码过期');
      case 'email_login': return t('邮箱登录');
      case 'email_config_change': return t('邮箱配置变更');
      case 'email_code_request': return t('请求验证码');
      case 'email_code_sent': return t('验证码已发送');
      case 'email_send_failed': return t('邮件发送失败');
      case 'email_verify': return t('邮箱验证');
      case 'email_verified': return t('邮箱验证通过');
      case 'captcha_failed': return t('验证码错误');
      case 'policy_change': return t('权限策略变更');
      case 'audit_prune': return t('审计清理');
      case 'offline_data_purge': return t('离线数据清理');
      case 'profile_update': return t('资料更新');
      case 'profile_avatar_update': return t('头像更新');
      case 'server_profile_update': return t('配置变更');
      case 'server_profile_avatar_update': return t('服务器头像更新');
      default: return action;
    }
  }

  Color _actionColor(String result) => result == 'success' ? AppTheme.successColor : AppTheme.errorColor;
}

String _resultLabel(String result) {
  switch (result) {
    case 'success': return t('成功');
    case 'denied': return t('拒绝');
    case 'failed': return t('失败');
    default: return result;
  }
}

class _ResultBadge extends StatelessWidget {
  final String result;
  const _ResultBadge({required this.result});
  @override
  Widget build(BuildContext context) {
    final color = result == 'success' ? AppTheme.successColor : AppTheme.errorColor;
    return Container(padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(color: color.withOpacity(0.15), borderRadius: BorderRadius.circular(10)),
      child: Text(_resultLabel(result), style: TextStyle(fontSize: 11, color: color)));
  }
}
