import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';

/// 服务端「数据保留」页（模块 7 UI 入口）。
/// 配置离线账户数据的保留策略，查看判定结果，并可立即执行一次清理。
class RetentionPage extends ConsumerStatefulWidget {
  const RetentionPage({super.key});

  @override
  ConsumerState<RetentionPage> createState() => _RetentionPageState();
}

class _RetentionPageState extends ConsumerState<RetentionPage> {
  bool _loading = true;
  bool _retain = true;
  final _offlineDays = TextEditingController();
  final _recycleDays = TextEditingController();
  final _auditDays = TextEditingController();
  List<Map<String, dynamic>> _decisions = [];

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _offlineDays.dispose();
    _recycleDays.dispose();
    _auditDays.dispose();
    super.dispose();
  }

  void _snack(String msg) {
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(msg)));
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    final api = ref.read(apiServiceProvider);
    try {
      final g = await api.getGlobalPolicy();
      final st = await api.getRetentionStatus();
      final p = (g['data'] as Map?)?.cast<String, dynamic>() ?? {};
      final items = (st['data']?['items'] as List?) ?? [];
      if (!mounted) return;
      setState(() {
        _retain = p['retain_offline_data'] != false;
        _offlineDays.text = '${p['offline_retention_days'] ?? 30}';
        _recycleDays.text = '${p['recycle_bin_retention_days'] ?? 30}';
        _auditDays.text = '${p['audit_retention_days'] ?? 30}';
        _decisions = items.map((e) => (e as Map).cast<String, dynamic>()).toList();
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _loading = false);
      _snack(t('加载失败: ${ApiService.describeError(e)}'));
    }
  }

  Future<void> _savePolicy() async {
    try {
      final g = await ref.read(apiServiceProvider).getGlobalPolicy();
      final p = (g['data'] as Map?)?.cast<String, dynamic>() ?? {};
      p['retain_offline_data'] = _retain;
      p['offline_retention_days'] = int.tryParse(_offlineDays.text.trim()) ?? 30;
      p['recycle_bin_retention_days'] = int.tryParse(_recycleDays.text.trim()) ?? 30;
      p['audit_retention_days'] = int.tryParse(_auditDays.text.trim()) ?? 30;
      await ref.read(apiServiceProvider).setGlobalPolicy(p);
      _snack(t('保留策略已保存'));
      _load();
    } catch (e) {
      _snack(t('保存失败: ${ApiService.describeError(e)}'));
    }
  }

  Future<void> _runNow() async {
    try {
      final resp = await ref.read(apiServiceProvider).runRetention();
      final purged = resp['data']?['purged'] ?? 0;
      _snack(t('已执行清理，清除 $purged 个离线账户数据'));
      _load();
    } catch (e) {
      _snack(t('执行失败: ${ApiService.describeError(e)}'));
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(t('数据保留'),
                  style: TextStyle(
                      fontSize: 24, fontWeight: FontWeight.bold, color: AppTheme.textPrimary)),
              const Spacer(),
              OutlinedButton.icon(
                onPressed: _runNow,
                icon: const Icon(Icons.cleaning_services, size: 18),
                label: Text(t('立即清理')),
              ),
              const SizedBox(width: 8),
              IconButton(icon: const Icon(Icons.refresh), tooltip: t('刷新'), onPressed: _load),
            ],
          ),
          const SizedBox(height: 4),
          Text(t('离线账户数据的保留时长，以及回收站/审计日志的自动清理周期。'),
              style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
          const SizedBox(height: 16),
          Expanded(
            child: _loading
                ? const Center(child: CircularProgressIndicator())
                : ListView(
                    children: [
                      Card(
                        child: Padding(
                          padding: const EdgeInsets.all(16),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              SwitchListTile(
                                contentPadding: EdgeInsets.zero,
                                dense: true,
                                title: Text(t('保留离线设备的私有数据')),
                                subtitle: Text(t('关闭后，设备一旦离线其私有数据将被清除'),
                                    style: const TextStyle(fontSize: 12)),
                                value: _retain,
                                onChanged: (v) => setState(() => _retain = v),
                              ),
                              const SizedBox(height: 8),
                              Row(
                                children: [
                                  Expanded(child: _num(_offlineDays, t('离线保留 (天，0=不限)'))),
                                  const SizedBox(width: 12),
                                  Expanded(child: _num(_recycleDays, t('回收站保留 (天)'))),
                                  const SizedBox(width: 12),
                                  Expanded(child: _num(_auditDays, t('审计日志保留 (天)'))),
                                ],
                              ),
                              const SizedBox(height: 16),
                              ElevatedButton.icon(
                                onPressed: _savePolicy,
                                icon: const Icon(Icons.save, size: 18),
                                label: Text(t('保存保留策略')),
                              ),
                            ],
                          ),
                        ),
                      ),
                      const SizedBox(height: 12),
                      Text(t('当前判定结果'),
                          style: TextStyle(fontWeight: FontWeight.w700, color: AppTheme.textPrimary)),
                      const SizedBox(height: 8),
                      if (_decisions.isEmpty)
                        Padding(
                          padding: const EdgeInsets.all(16),
                          child: Text(t('暂无设备'),
                              style: TextStyle(color: AppTheme.textSecondary)),
                        )
                      else
                        ..._decisions.map((d) {
                          final purge = d['purge'] == true;
                          return Card(
                            margin: const EdgeInsets.only(bottom: 8),
                            child: ListTile(
                              leading: Icon(
                                purge ? Icons.delete_forever : Icons.verified_user,
                                color: purge ? AppTheme.errorColor : AppTheme.successColor,
                              ),
                              title: Text('${d['device_id']}',
                                  style: TextStyle(color: AppTheme.textPrimary)),
                              subtitle: Text('${d['reason']}',
                                  style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
                              trailing: Text(
                                purge ? t('将清除') : t('保留'),
                                style: TextStyle(
                                    color: purge ? AppTheme.errorColor : AppTheme.successColor,
                                    fontWeight: FontWeight.w600),
                              ),
                            ),
                          );
                        }),
                      const SizedBox(height: 24),
                    ],
                  ),
          ),
        ],
      ),
    );
  }

  Widget _num(TextEditingController c, String label) => TextField(
        controller: c,
        keyboardType: TextInputType.number,
        decoration: InputDecoration(
          labelText: label,
          isDense: true,
          border: const OutlineInputBorder(),
        ),
      );
}
