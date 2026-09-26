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
              title: Text('${log.action} · ${log.deviceId}', style: TextStyle(fontSize: 13, color: AppTheme.textPrimary)),
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

  Color _actionColor(String result) => result == 'success' ? AppTheme.successColor : AppTheme.errorColor;
}

class _ResultBadge extends StatelessWidget {
  final String result;
  const _ResultBadge({required this.result});
  @override
  Widget build(BuildContext context) {
    final color = result == 'success' ? AppTheme.successColor : AppTheme.errorColor;
    return Container(padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(color: color.withOpacity(0.15), borderRadius: BorderRadius.circular(10)),
      child: Text(result, style: TextStyle(fontSize: 11, color: color)));
  }
}
