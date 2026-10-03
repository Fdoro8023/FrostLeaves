import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';

/// 服务端「账号与权限」页（模块 3 UI 入口）。
/// 三级策略：全局默认 < 账号覆盖 < 设备覆盖；另含在线会话（可强制下线）。
class AccountPolicyPage extends ConsumerStatefulWidget {
  const AccountPolicyPage({super.key});

  @override
  ConsumerState<AccountPolicyPage> createState() => _AccountPolicyPageState();
}

class _AccountPolicyPageState extends ConsumerState<AccountPolicyPage>
    with SingleTickerProviderStateMixin {
  late TabController _tabs;
  bool _loading = true;
  Map<String, dynamic> _global = {};
  Map<String, dynamic> _accounts = {};
  Map<String, dynamic> _devices = {};
  List<Map<String, dynamic>> _sessions = [];

  @override
  void initState() {
    super.initState();
    _tabs = TabController(length: 4, vsync: this);
    _tabs.addListener(() {
      if (!_tabs.indexIsChanging && mounted) _load();
    });
    _load();
  }

  @override
  void dispose() {
    _tabs.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    final api = ref.read(apiServiceProvider);
    try {
      final g = await api.getGlobalPolicy();
      final a = await api.getAccountPolicies();
      final d = await api.getDevicePolicies();
      final s = await api.getSessions();
      if (!mounted) return;
      setState(() {
        _global = (g['data'] as Map?)?.cast<String, dynamic>() ?? {};
        _accounts = (a['data'] as Map?)?.cast<String, dynamic>() ?? {};
        _devices = (d['data'] as Map?)?.cast<String, dynamic>() ?? {};
        final items = (s['data']?['items'] as List?) ?? [];
        _sessions = items.map((e) => (e as Map).cast<String, dynamic>()).toList();
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _loading = false);
      _snack(t('加载失败: ${ApiService.describeError(e)}'));
    }
  }

  void _snack(String msg) {
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(msg)));
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
              Text(t('账号与权限'),
                  style: TextStyle(
                      fontSize: 24, fontWeight: FontWeight.bold, color: AppTheme.textPrimary)),
              const Spacer(),
              IconButton(icon: const Icon(Icons.refresh), tooltip: t('刷新'), onPressed: _load),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            t('优先级：设备 > 账号 > 全局；未覆盖的字段自动继承下一级。'),
            style: TextStyle(fontSize: 12, color: AppTheme.textSecondary),
          ),
          const SizedBox(height: 12),
          TabBar(
            controller: _tabs,
            tabs: [
              Tab(text: t('全局策略')),
              Tab(text: t('账号覆盖 (${_accounts.length})')),
              Tab(text: t('设备覆盖 (${_devices.length})')),
              Tab(text: t('在线会话 (${_sessions.length})')),
            ],
          ),
          const SizedBox(height: 16),
          Expanded(
            child: _loading
                ? const Center(child: CircularProgressIndicator())
                : TabBarView(
                    controller: _tabs,
                    children: [
                      _GlobalPolicyTab(
                        policy: _global,
                        onSave: (p) async {
                          try {
                            await ref.read(apiServiceProvider).setGlobalPolicy(p);
                            _snack(t('全局策略已保存'));
                            _load();
                          } catch (e) {
                            _snack(t('保存失败: ${ApiService.describeError(e)}'));
                          }
                        },
                      ),
                      _OverrideListTab(
                        entries: _accounts,
                        idLabel: t('账号 ID'),
                        deviceMode: false,
                        onEdit: (id, override) async {
                          try {
                            await ref.read(apiServiceProvider).setAccountPolicy(id, override);
                            _snack(t('账号策略已保存'));
                            _load();
                          } catch (e) {
                            _snack(t('保存失败: ${ApiService.describeError(e)}'));
                          }
                        },
                      ),
                      _OverrideListTab(
                        entries: _devices,
                        idLabel: t('设备 ID'),
                        deviceMode: true,
                        onEdit: (id, override) async {
                          try {
                            await ref.read(apiServiceProvider).setDevicePolicy(id, override);
                            _snack(t('设备策略已保存'));
                            _load();
                          } catch (e) {
                            _snack(t('保存失败: ${ApiService.describeError(e)}'));
                          }
                        },
                      ),
                      _SessionsTab(
                        sessions: _sessions,
                        onLogout: (deviceId) async {
                          try {
                            await ref.read(apiServiceProvider).forceLogout(deviceId);
                            _snack(t('已强制下线'));
                            _load();
                          } catch (e) {
                            _snack(t('操作失败: ${ApiService.describeError(e)}'));
                          }
                        },
                      ),
                    ],
                  ),
          ),
        ],
      ),
    );
  }
}

// ===================== 全局策略 =====================

class _GlobalPolicyTab extends StatefulWidget {
  final Map<String, dynamic> policy;
  final Future<void> Function(Map<String, dynamic>) onSave;
  const _GlobalPolicyTab({required this.policy, required this.onSave});

  @override
  State<_GlobalPolicyTab> createState() => _GlobalPolicyTabState();
}

class _GlobalPolicyTabState extends State<_GlobalPolicyTab> {
  late Map<String, dynamic> _p;

  @override
  void initState() {
    super.initState();
    _p = Map<String, dynamic>.from(widget.policy);
  }

  @override
  void didUpdateWidget(covariant _GlobalPolicyTab old) {
    super.didUpdateWidget(old);
    if (old.policy != widget.policy && widget.policy.isNotEmpty) {
      _p = Map<String, dynamic>.from(widget.policy);
    }
  }

  bool _b(String k, {bool def = false}) => _p[k] == true || (_p[k] == null && def);
  int _i(String k) => (_p[k] is int) ? _p[k] as int : 0;
  String _csv(String k) => (_p[k] as List?)?.join(', ') ?? '';

  @override
  Widget build(BuildContext context) {
    if (_p.isEmpty) {
      return Center(child: Text(t('暂无数据'), style: TextStyle(color: AppTheme.textSecondary)));
    }
    return ListView(
      children: [
        _section(t('存储与文件')),
        _numField(t('总存储配额 (GB，0=不限)'), 'storage_quota_bytes', gb: true),
        _numField(t('单文件上限 (MB，0=不限)'), 'max_file_size_bytes', mb: true),
        _numField(t('文件数量上限 (0=不限)'), 'max_file_count'),
        _textField(t('允许的扩展名 (逗号分隔，留空=不限)'), 'allowed_exts', csv: true),
        _textField(t('禁止的扩展名 (逗号分隔)'), 'denied_exts', csv: true),
        _switchField(t('校验文件类型（魔数）'), 'magic_check'),
        _switchField(t('允许可执行文件（开发者）'), 'allow_executable'),
        _section(t('带宽')),
        _numField(t('上传限速 (KB/s，0=不限)'), 'upload_speed_kbps'),
        _numField(t('下载限速 (KB/s，0=不限)'), 'download_speed_kbps'),
        _section(t('操作开关')),
        _switchField(t('允许上传'), 'allow_upload'),
        _switchField(t('允许下载'), 'allow_download'),
        _switchField(t('允许删除'), 'allow_delete'),
        _switchField(t('允许重命名'), 'allow_rename'),
        _switchField(t('允许新建文件夹'), 'allow_mkdir'),
        _section(t('分享')),
        _switchField(t('允许分享'), 'allow_share'),
        _numField(t('分享最大下载次数 (0=不限)'), 'share_max_downloads'),
        _numField(t('分享有效期 (小时)'), 'share_expiry_hours'),
        _section(t('会话与审计')),
        _numField(t('每账号最大设备数 (0=不限)'), 'max_devices_per_account'),
        _numField(t('审计日志保留 (天)'), 'audit_retention_days'),
        _section(t('回收站（模块 2）')),
        _switchField(t('回收站占用计入配额'), 'recycle_bin_counts_quota'),
        _numField(t('回收站保留 (天，0=永久)'), 'recycle_bin_retention_days'),
        _section(t('离线数据保留（模块 7）')),
        _switchField(t('保留离线设备的私有数据'), 'retain_offline_data'),
        _numField(t('离线保留时长 (天，0=不限)'), 'offline_retention_days'),
        const SizedBox(height: 20),
        Row(
          children: [
            ElevatedButton.icon(
              onPressed: () => widget.onSave(_p),
              icon: const Icon(Icons.save, size: 18),
              label: Text(t('保存全局策略')),
            ),
          ],
        ),
        const SizedBox(height: 24),
      ],
    );
  }

  Widget _section(String title) => Padding(
        padding: const EdgeInsets.only(top: 16, bottom: 4),
        child: Text(title,
            style: TextStyle(
                fontSize: 14, fontWeight: FontWeight.w700, color: AppTheme.primaryColor)),
      );

  Widget _numField(String label, String key, {bool gb = false, bool mb = false}) {
    final raw = _i(key);
    final value = gb
        ? (raw / (1024 * 1024 * 1024))
        : mb
            ? (raw / (1024 * 1024))
            : raw.toDouble();
    final text = (value == value.roundToDouble())
        ? value.round().toString()
        : value.toStringAsFixed(2);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        children: [
          Expanded(child: Text(label, style: TextStyle(color: AppTheme.textPrimary))),
          SizedBox(
            width: 140,
            child: TextFormField(
              key: ValueKey('$key-$text'),
              initialValue: text,
              keyboardType: const TextInputType.numberWithOptions(decimal: true),
              decoration: const InputDecoration(isDense: true, border: OutlineInputBorder()),
              onChanged: (v) {
                final n = double.tryParse(v.trim()) ?? 0;
                _p[key] = gb
                    ? (n * 1024 * 1024 * 1024).round()
                    : mb
                        ? (n * 1024 * 1024).round()
                        : n.round();
              },
            ),
          ),
        ],
      ),
    );
  }

  Widget _textField(String label, String key, {bool csv = false}) => Padding(
        padding: const EdgeInsets.symmetric(vertical: 6),
        child: TextFormField(
          initialValue: _csv(key),
          decoration: InputDecoration(
            labelText: label,
            isDense: true,
            border: const OutlineInputBorder(),
          ),
          onChanged: (v) {
            if (csv) {
              _p[key] = v
                  .split(',')
                  .map((e) => e.trim())
                  .where((e) => e.isNotEmpty)
                  .toList();
            } else {
              _p[key] = v.trim();
            }
          },
        ),
      );

  Widget _switchField(String label, String key) => SwitchListTile(
        contentPadding: EdgeInsets.zero,
        dense: true,
        title: Text(label, style: TextStyle(color: AppTheme.textPrimary)),
        value: _b(key),
        onChanged: (v) => setState(() => _p[key] = v),
      );
}

// ===================== 账号 / 设备覆盖 =====================

class _OverrideListTab extends StatelessWidget {
  final Map<String, dynamic> entries;
  final String idLabel;
  final bool deviceMode;
  final Future<void> Function(String id, Map<String, dynamic>? override) onEdit;
  const _OverrideListTab({
    required this.entries,
    required this.idLabel,
    required this.deviceMode,
    required this.onEdit,
  });

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        Row(
          children: [
            Expanded(
              child: Text(
                t('为该级设置覆盖项；未填写的字段继承下一级。清除即移除该条覆盖。'),
                style: TextStyle(fontSize: 12, color: AppTheme.textSecondary),
              ),
            ),
            const SizedBox(width: 8),
            ElevatedButton.icon(
              onPressed: () => _openEditor(context, null, null),
              icon: const Icon(Icons.add, size: 18),
              label: Text(t('新增覆盖')),
            ),
          ],
        ),
        const SizedBox(height: 12),
        Expanded(
          child: entries.isEmpty
              ? Center(child: Text(t('暂无覆盖项'), style: TextStyle(color: AppTheme.textSecondary)))
              : ListView(
                  children: entries.entries.map((e) {
                    final ov = (e.value as Map?)?.cast<String, dynamic>() ?? {};
                    return Card(
                      margin: const EdgeInsets.only(bottom: 8),
                      child: ListTile(
                        title: Text(e.key, style: TextStyle(color: AppTheme.textPrimary)),
                        subtitle: Text(_summarize(ov),
                            style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
                        trailing: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            TextButton(
                              onPressed: () => _openEditor(context, e.key, ov),
                              child: Text(t('编辑')),
                            ),
                            TextButton(
                              onPressed: () => onEdit(e.key, null),
                              child: Text(t('清除'), style: TextStyle(color: AppTheme.errorColor)),
                            ),
                          ],
                        ),
                      ),
                    );
                  }).toList(),
                ),
        ),
      ],
    );
  }

  String _summarize(Map<String, dynamic> ov) {
    if (ov.isEmpty) return t('（继承）');
    final parts = <String>[];
    if (ov['storage_quota_bytes'] != null) {
      parts.add('${t('配额')}=${_gb(ov['storage_quota_bytes'])}GB');
    }
    if (ov['max_file_size_bytes'] != null) {
      parts.add('${t('单文件')}=${_mb(ov['max_file_size_bytes'])}MB');
    }
    if (ov['allow_executable'] != null) {
      parts.add('${t('可执行')}=${ov['allow_executable'] == true ? t('允许') : t('禁止')}');
    }
    if (ov['recycle_bin_retention_days'] != null) {
      parts.add('${t('回收站')}=${ov['recycle_bin_retention_days']}${t('天')}');
    }
    if (ov['offline_retention_days'] != null) {
      parts.add('${t('离线保留')}=${ov['offline_retention_days']}${t('天')}');
    }
    return parts.isEmpty ? t('（继承）') : parts.join(' · ');
  }

  String _gb(dynamic v) => ((v as num) / (1024 * 1024 * 1024)).toStringAsFixed(v % (1024 * 1024 * 1024) == 0 ? 0 : 1);
  String _mb(dynamic v) => ((v as num) / (1024 * 1024)).toStringAsFixed(1);

  Future<void> _openEditor(BuildContext context, String? id, Map<String, dynamic>? existing) async {
    final result = await showDialog<Map<String, dynamic>?>(
      context: context,
      builder: (_) => _OverrideEditor(id: id, existing: existing ?? {}, deviceMode: deviceMode),
    );
    if (result == null) return;
    await onEdit(result['id'] as String, result['override'] as Map<String, dynamic>);
  }
}

/// 覆盖项编辑器：只写入用户显式填写的字段（其余为 null = 继承）。
class _OverrideEditor extends ConsumerStatefulWidget {
  final String? id;
  final Map<String, dynamic> existing;
  final bool deviceMode;
  const _OverrideEditor({this.id, required this.existing, this.deviceMode = false});

  @override
  ConsumerState<_OverrideEditor> createState() => _OverrideEditorState();
}

class _OverrideEditorState extends ConsumerState<_OverrideEditor> {
  late final TextEditingController _id;
  bool _useQuota = false, _useMaxFile = false, _useUp = false, _useDown = false;
  bool _useCount = false, _useExec = false, _useRecycle = false, _useOffline = false;
  final _quota = TextEditingController();
  final _maxFile = TextEditingController();
  final _up = TextEditingController();
  final _down = TextEditingController();
  final _count = TextEditingController();
  final _recycle = TextEditingController();
  final _offline = TextEditingController();
  bool _exec = true;

  @override
  void initState() {
    super.initState();
    _id = TextEditingController(text: widget.id ?? '');
    final o = widget.existing;
    if (o['storage_quota_bytes'] != null) {
      _useQuota = true;
      _quota.text = ((o['storage_quota_bytes'] as num) / (1024 * 1024 * 1024)).toString();
    }
    if (o['max_file_size_bytes'] != null) {
      _useMaxFile = true;
      _maxFile.text = ((o['max_file_size_bytes'] as num) / (1024 * 1024)).toString();
    }
    if (o['upload_speed_kbps'] != null) {
      _useUp = true;
      _up.text = '${o['upload_speed_kbps']}';
    }
    if (o['download_speed_kbps'] != null) {
      _useDown = true;
      _down.text = '${o['download_speed_kbps']}';
    }
    if (o['max_file_count'] != null) {
      _useCount = true;
      _count.text = '${o['max_file_count']}';
    }
    if (o['allow_executable'] != null) {
      _useExec = true;
      _exec = o['allow_executable'] == true;
    }
    if (o['recycle_bin_retention_days'] != null) {
      _useRecycle = true;
      _recycle.text = '${o['recycle_bin_retention_days']}';
    }
    if (o['offline_retention_days'] != null) {
      _useOffline = true;
      _offline.text = '${o['offline_retention_days']}';
    }
  }

  @override
  void dispose() {
    for (final c in [_id, _quota, _maxFile, _up, _down, _count, _recycle, _offline]) {
      c.dispose();
    }
    super.dispose();
  }

  /// 项目 5：点击下拉箭头时实时拉取服务器上的设备 / 账号列表并选中填入。
  Future<void> _pickId() async {
    final picked = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(widget.deviceMode ? t('选择设备') : t('选择账号')),
        content: SizedBox(
          width: 440,
          height: 380,
          child: FutureBuilder<List<Map<String, String>>>(
            future: _loadOptions(),
            builder: (c, snap) {
              if (snap.connectionState != ConnectionState.done) {
                return const Center(child: CircularProgressIndicator());
              }
              if (snap.hasError) {
                return Center(
                    child: Text(t('加载失败：${ApiService.describeError(snap.error!)}')));
              }
              final list = snap.data ?? [];
              if (list.isEmpty) {
                return Center(child: Text(t('当前没有可选项')));
              }
              return ListView(
                children: list
                    .map((o) => ListTile(
                          dense: true,
                          title: Text(o['id'] ?? ''),
                          subtitle: Text(o['label'] ?? ''),
                          onTap: () => Navigator.pop(ctx, o['id']),
                        ))
                    .toList(),
              );
            },
          ),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: Text(t('取消'))),
        ],
      ),
    );
    if (picked != null && picked.isNotEmpty && mounted) {
      setState(() => _id.text = picked);
    }
  }

  /// 设备模式：已连接设备列表；账号模式：已有账号覆盖 + 在线会话里的账号 ID。
  Future<List<Map<String, String>>> _loadOptions() async {
    final api = ref.read(apiServiceProvider);
    await api.loadSavedCredentials();
    final out = <Map<String, String>>[];
    final seen = <String>{};
    if (widget.deviceMode) {
      final resp = await api.listDevices();
      final data = resp['data'] as Map<String, dynamic>?;
      final items = (data?['items'] as List?) ?? [];
      for (final e in items) {
        final m = (e as Map).cast<String, dynamic>();
        final id = '${m['id'] ?? ''}';
        if (id.isEmpty || !seen.add(id)) continue;
        final name = '${m['name'] ?? ''}';
        out.add({
          'id': id,
          'label': '${name.isEmpty ? "-" : name} · ${m['platform'] ?? ''} · ${m['status'] ?? ''}',
        });
      }
      return out;
    }
    final accounts = await api.getAccountPolicies();
    final map = (accounts['data'] as Map?)?.cast<String, dynamic>() ?? {};
    for (final k in map.keys) {
      if (seen.add(k)) out.add({'id': k, 'label': t('已有账号覆盖')});
    }
    try {
      final sess = await api.getSessions();
      final items = (sess['data']?['items'] as List?) ?? [];
      for (final e in items) {
        final m = (e as Map).cast<String, dynamic>();
        final aid = '${m['account_id'] ?? ''}';
        if (aid.isEmpty || !seen.add(aid)) continue;
        out.add({'id': aid, 'label': '${t('在线设备')}: ${m['device_id'] ?? ''}'});
      }
    } catch (_) {}
    return out;
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text(t('编辑覆盖项')),
      content: SizedBox(
        width: 420,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: _id,
                enabled: widget.id == null,
                decoration: InputDecoration(
                  labelText: widget.deviceMode ? t('设备 ID') : t('账号 ID'),
                  isDense: true,
                  border: const OutlineInputBorder(),
                  suffixIcon: widget.id == null
                      ? IconButton(
                          icon: const Icon(Icons.arrow_drop_down),
                          tooltip: widget.deviceMode ? t('选择设备') : t('选择账号'),
                          onPressed: _pickId,
                        )
                      : null,
                ),
              ),
              const SizedBox(height: 12),
              _row(t('总存储配额 (GB)'), _quota, _useQuota, (v) => _useQuota = v),
              _row(t('单文件上限 (MB)'), _maxFile, _useMaxFile, (v) => _useMaxFile = v),
              _row(t('上传限速 (KB/s)'), _up, _useUp, (v) => _useUp = v),
              _row(t('下载限速 (KB/s)'), _down, _useDown, (v) => _useDown = v),
              _row(t('文件数量上限'), _count, _useCount, (v) => _useCount = v),
              _row(t('回收站保留 (天)'), _recycle, _useRecycle, (v) => _useRecycle = v),
              _row(t('离线保留 (天)'), _offline, _useOffline, (v) => _useOffline = v),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                dense: true,
                title: Text(t('覆盖「允许可执行文件」')),
                value: _useExec,
                onChanged: (v) => setState(() => _useExec = v),
              ),
              if (_useExec)
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  dense: true,
                  title: Text(t('允许可执行文件')),
                  value: _exec,
                  onChanged: (v) => setState(() => _exec = v),
                ),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(context, null), child: Text(t('取消'))),
        TextButton(
          onPressed: () {
            final id = _id.text.trim();
            if (id.isEmpty) return;
            final ov = <String, dynamic>{};
            if (_useQuota) {
              ov['storage_quota_bytes'] =
                  ((double.tryParse(_quota.text) ?? 0) * 1024 * 1024 * 1024).round();
            }
            if (_useMaxFile) {
              ov['max_file_size_bytes'] =
                  ((double.tryParse(_maxFile.text) ?? 0) * 1024 * 1024).round();
            }
            if (_useUp) ov['upload_speed_kbps'] = int.tryParse(_up.text) ?? 0;
            if (_useDown) ov['download_speed_kbps'] = int.tryParse(_down.text) ?? 0;
            if (_useCount) ov['max_file_count'] = int.tryParse(_count.text) ?? 0;
            if (_useRecycle) ov['recycle_bin_retention_days'] = int.tryParse(_recycle.text) ?? 0;
            if (_useOffline) ov['offline_retention_days'] = int.tryParse(_offline.text) ?? 0;
            if (_useExec) ov['allow_executable'] = _exec;
            Navigator.pop(context, {'id': id, 'override': ov});
          },
          child: Text(t('保存')),
        ),
      ],
    );
  }

  Widget _row(String label, TextEditingController c, bool enabled, ValueChanged<bool> onToggle) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        children: [
          Checkbox(value: enabled, onChanged: (v) => setState(() => onToggle(v ?? false))),
          Expanded(child: Text(label, style: const TextStyle(fontSize: 13))),
          SizedBox(
            width: 110,
            child: TextField(
              controller: c,
              enabled: enabled,
              keyboardType: const TextInputType.numberWithOptions(decimal: true),
              decoration: const InputDecoration(isDense: true, border: OutlineInputBorder()),
            ),
          ),
        ],
      ),
    );
  }
}

// ===================== 在线会话 =====================

class _SessionsTab extends StatelessWidget {
  final List<Map<String, dynamic>> sessions;
  final Future<void> Function(String deviceId) onLogout;
  const _SessionsTab({required this.sessions, required this.onLogout});

  @override
  Widget build(BuildContext context) {
    if (sessions.isEmpty) {
      return Center(child: Text(t('当前没有在线会话'), style: TextStyle(color: AppTheme.textSecondary)));
    }
    return ListView(
      children: sessions.map((s) {
        final deviceId = '${s['device_id'] ?? ''}';
        return Card(
          margin: const EdgeInsets.only(bottom: 8),
          child: ListTile(
            leading: Icon(Icons.link, color: AppTheme.primaryColor),
            title: Text(
              '${(s['device_name'] ?? '').toString().isEmpty ? deviceId : s['device_name']}',
              style: TextStyle(color: AppTheme.textPrimary),
            ),
            subtitle: Text(
              'ID: $deviceId · ${s['platform'] ?? ''}'
              '${(s['account_id'] ?? '').toString().isNotEmpty ? ' · ${t('账号')}: ${s['account_id']}' : ''}',
              style: TextStyle(fontSize: 12, color: AppTheme.textSecondary),
            ),
            trailing: OutlinedButton(
              onPressed: () => onLogout(deviceId),
              style: OutlinedButton.styleFrom(
                foregroundColor: AppTheme.errorColor,
                side: BorderSide(color: AppTheme.errorColor),
              ),
              child: Text(t('强制下线')),
            ),
          ),
        );
      }).toList(),
    );
  }
}
