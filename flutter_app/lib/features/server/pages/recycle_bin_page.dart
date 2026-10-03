import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';

/// 服务端「回收站」页（模块 2 UI 入口）。
/// 数据来自文件网关 /api/v1/recycle/*（服务端模式自动走回环）。
class ServerRecycleBinPage extends ConsumerStatefulWidget {
  const ServerRecycleBinPage({super.key});

  @override
  ConsumerState<ServerRecycleBinPage> createState() => _ServerRecycleBinPageState();
}

class _ServerRecycleBinPageState extends ConsumerState<ServerRecycleBinPage> {
  List<ServerRecycleItem> _items = [];
  bool _loading = true;
  Set<String> _selectedIds = {};

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    try {
      final api = ref.read(apiServiceProvider);
      final data = await api.getRecycleList();
      final itemsData = (data['data']?['items'] as List?) ?? [];
      setState(() {
        _items = itemsData
            .map((e) => ServerRecycleItem.fromJson(e as Map<String, dynamic>))
            .toList();
        _loading = false;
      });
    } catch (e) {
      setState(() => _loading = false);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t('加载失败: ${ApiService.describeError(e)}'))),
        );
      }
    }
  }

  Future<void> _restore(ServerRecycleItem item) async {
    final ok = await _confirm(t('还原文件'), t('确定要还原 "${item.displayName}" 吗？'));
    if (ok != true) return;
    try {
      await ref.read(apiServiceProvider).restoreFromRecycle(item.id);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t('已还原'))));
        _load();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t('还原失败: ${ApiService.describeError(e)}'))),
        );
      }
    }
  }

  Future<void> _deletePermanent(List<String> ids) async {
    if (ids.isEmpty) return;
    final ok = await _confirm(
      t('永久删除'),
      t('确定要永久删除选中的 ${ids.length} 个条目吗？此操作不可恢复！'),
      danger: true,
    );
    if (ok != true) return;
    try {
      await ref.read(apiServiceProvider).deleteFromRecycle(ids);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t('已永久删除'))));
        setState(() => _selectedIds.clear());
        _load();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t('删除失败: ${ApiService.describeError(e)}'))),
        );
      }
    }
  }

  Future<void> _clearAll() async {
    final ok = await _confirm(
      t('清空回收站'),
      t('确定要清空回收站吗？所有条目将永久删除，此操作不可恢复！'),
      danger: true,
    );
    if (ok != true) return;
    try {
      await ref.read(apiServiceProvider).clearRecycle();
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t('回收站已清空'))));
        _load();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t('清空失败: ${ApiService.describeError(e)}'))),
        );
      }
    }
  }

  Future<bool?> _confirm(String title, String body, {bool danger = false}) {
    return showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(title),
        content: Text(body),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: Text(t('取消'))),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(
              danger ? t('确认删除') : t('确定'),
              style: danger ? TextStyle(color: AppTheme.errorColor) : null,
            ),
          ),
        ],
      ),
    );
  }

  String _formatSize(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    if (bytes < 1024 * 1024 * 1024) return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
    return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(2)} GB';
  }

  String _formatDate(String s) {
    try {
      final d = DateTime.parse(s).toLocal();
      String two(int v) => v.toString().padLeft(2, '0');
      return '${d.year}-${two(d.month)}-${two(d.day)} ${two(d.hour)}:${two(d.minute)}';
    } catch (_) {
      return s;
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
              Text(
                t('回收站'),
                style: TextStyle(
                    fontSize: 24, fontWeight: FontWeight.bold, color: AppTheme.textPrimary),
              ),
              const SizedBox(width: 12),
              Text('${_items.length} ${t('个条目')}',
                  style: TextStyle(fontSize: 13, color: AppTheme.textSecondary)),
              const Spacer(),
              if (_selectedIds.isNotEmpty) ...[
                OutlinedButton.icon(
                  onPressed: () => _deletePermanent(_selectedIds.toList()),
                  icon: Icon(Icons.delete_forever, size: 18, color: AppTheme.errorColor),
                  label: Text(t('永久删除选中 (${_selectedIds.length})'),
                      style: TextStyle(color: AppTheme.errorColor)),
                ),
                const SizedBox(width: 8),
              ],
              OutlinedButton.icon(
                onPressed: _items.isEmpty ? null : _clearAll,
                icon: Icon(Icons.delete_sweep, size: 18, color: AppTheme.errorColor),
                label: Text(t('清空'), style: TextStyle(color: AppTheme.errorColor)),
              ),
              const SizedBox(width: 8),
              IconButton(
                icon: const Icon(Icons.refresh),
                tooltip: t('刷新'),
                onPressed: _load,
              ),
            ],
          ),
          const SizedBox(height: 8),
          Text(
            t('删除的文件会先进入回收站；保留时长与「回收站是否计入配额」在「全局策略」中配置。'),
            style: TextStyle(fontSize: 12, color: AppTheme.textSecondary),
          ),
          const SizedBox(height: 16),
          Expanded(
            child: _loading
                ? const Center(child: CircularProgressIndicator())
                : _items.isEmpty
                    ? Center(
                        child: Column(
                          mainAxisAlignment: MainAxisAlignment.center,
                          children: [
                            Icon(Icons.delete_outline, size: 64, color: AppTheme.textSecondary),
                            const SizedBox(height: 12),
                            Text(t('回收站为空'),
                                style: TextStyle(color: AppTheme.textSecondary)),
                          ],
                        ),
                      )
                    : RefreshIndicator(
                        onRefresh: _load,
                        child: ListView.builder(
                          itemCount: _items.length,
                          itemBuilder: (context, index) {
                            final item = _items[index];
                            final selected = _selectedIds.contains(item.id);
                            return Card(
                              margin: const EdgeInsets.only(bottom: 8),
                              color: selected ? AppTheme.primaryColor.withOpacity(0.10) : null,
                              child: ListTile(
                                leading: Icon(
                                  item.isDirectory ? Icons.folder : Icons.insert_drive_file,
                                  color: selected ? AppTheme.primaryColor : AppTheme.textSecondary,
                                ),
                                title: Text(
                                  item.displayName,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: TextStyle(color: AppTheme.textPrimary),
                                ),
                                subtitle: Text(
                                  '${_formatSize(item.size)} · ${_formatDate(item.deletedAt)}'
                                  '${item.originalPath.isNotEmpty ? ' · ${item.originalPath}' : ''}',
                                  style: TextStyle(fontSize: 12, color: AppTheme.textSecondary),
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                ),
                                trailing: Row(
                                  mainAxisSize: MainAxisSize.min,
                                  children: [
                                    TextButton.icon(
                                      onPressed: () => _restore(item),
                                      icon: const Icon(Icons.restore, size: 18),
                                      label: Text(t('还原')),
                                    ),
                                    TextButton.icon(
                                      onPressed: () => _deletePermanent([item.id]),
                                      icon: Icon(Icons.delete_forever,
                                          size: 18, color: AppTheme.errorColor),
                                      label: Text(t('删除'),
                                          style: TextStyle(color: AppTheme.errorColor)),
                                    ),
                                  ],
                                ),
                                onTap: () {
                                  setState(() {
                                    if (selected) {
                                      _selectedIds.remove(item.id);
                                    } else {
                                      _selectedIds.add(item.id);
                                    }
                                  });
                                },
                              ),
                            );
                          },
                        ),
                      ),
          ),
        ],
      ),
    );
  }
}

class ServerRecycleItem {
  final String id;
  final String name;
  final String originalPath;
  final String originalName;
  final String deletedAt;
  final int size;
  final bool isDirectory;

  ServerRecycleItem({
    required this.id,
    required this.name,
    required this.originalPath,
    required this.originalName,
    required this.deletedAt,
    required this.size,
    required this.isDirectory,
  });

  String get displayName => originalName.isNotEmpty ? originalName : name;

  factory ServerRecycleItem.fromJson(Map<String, dynamic> json) {
    return ServerRecycleItem(
      id: (json['id'] ?? '').toString(),
      name: (json['name'] ?? '').toString(),
      originalPath: (json['original_path'] ?? '').toString(),
      originalName: (json['original_name'] ?? '').toString(),
      deletedAt: (json['deleted_at'] ?? '').toString(),
      size: (json['size'] is int) ? json['size'] as int : 0,
      isDirectory: json['is_directory'] == true,
    );
  }
}
