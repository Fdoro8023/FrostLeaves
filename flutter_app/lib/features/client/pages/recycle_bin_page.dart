import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/services/api_service.dart';

class RecycleBinPage extends ConsumerStatefulWidget {
  const RecycleBinPage({super.key});

  @override
  ConsumerState<RecycleBinPage> createState() => _RecycleBinPageState();
}

class _RecycleBinPageState extends ConsumerState<RecycleBinPage> {
  List<RecycleItem> _items = [];
  bool _loading = true;
  Set<String> _selectedIds = {};

  @override
  void initState() {
    super.initState();
    _loadRecycleList();
  }

  Future<void> _loadRecycleList() async {
    setState(() => _loading = true);
    try {
      final api = ref.read(apiServiceProvider);
      final data = await api.getRecycleList();
      final itemsData = data['data']?['items'] as List? ?? [];
      setState(() {
        _items = itemsData.map((e) => RecycleItem.fromJson(e)).toList();
        _loading = false;
      });
    } catch (e) {
      setState(() => _loading = false);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t('加载失败: $e'))),
        );
      }
    }
  }

  Future<void> _restoreItem(String id, String name) async {
    final confirm = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t('还原文件')),
        content: Text(t('确定要还原 "$name" 吗？')),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: Text(t('取消'))),
          TextButton(onPressed: () => Navigator.pop(ctx, true), child: Text(t('还原'))),
        ],
      ),
    );

    if (confirm != true) return;

    try {
      final api = ref.read(apiServiceProvider);
      await api.restoreFromRecycle(id);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t('还原成功'))),
        );
        _loadRecycleList();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t('还原失败: $e'))),
        );
      }
    }
  }

  Future<void> _deletePermanently(List<String> ids) async {
    final confirm = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t('永久删除')),
        content: Text(t('确定要永久删除选中的 ${ids.length} 个文件吗？此操作不可恢复！')),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: Text(t('取消'))),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(t('永久删除'), style: TextStyle(color: Colors.red)),
          ),
        ],
      ),
    );

    if (confirm != true) return;

    try {
      final api = ref.read(apiServiceProvider);
      await api.deleteFromRecycle(ids);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t('已永久删除'))),
        );
        setState(() {
          _selectedIds.clear();
        });
        _loadRecycleList();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t('删除失败: $e'))),
        );
      }
    }
  }

  Future<void> _clearAll() async {
    final confirm = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t('清空回收站')),
        content: Text(t('确定要清空回收站吗？所有文件将永久删除，此操作不可恢复！')),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: Text(t('取消'))),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(t('清空'), style: TextStyle(color: Colors.red)),
          ),
        ],
      ),
    );

    if (confirm != true) return;

    try {
      final api = ref.read(apiServiceProvider);
      await api.clearRecycle();
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t('回收站已清空'))),
        );
        _loadRecycleList();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t('清空失败: $e'))),
        );
      }
    }
  }

  String _formatSize(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    if (bytes < 1024 * 1024 * 1024) return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
    return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(2)} GB';
  }

  String _formatDate(String dateStr) {
    try {
      final date = DateTime.parse(dateStr);
      return '${date.year}-${date.month.toString().padLeft(2, '0')}-${date.day.toString().padLeft(2, '0')} ${date.hour.toString().padLeft(2, '0')}:${date.minute.toString().padLeft(2, '0')}';
    } catch (e) {
      return dateStr;
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(t('回收站')),
        actions: [
          if (_selectedIds.isNotEmpty)
            IconButton(
              icon: const Icon(Icons.delete_forever, color: Colors.red),
              onPressed: () => _deletePermanently(_selectedIds.toList()),
              tooltip: t('永久删除选中')),
          IconButton(
            icon: const Icon(Icons.delete_sweep),
            onPressed: _clearAll,
            tooltip: t('清空回收站')),
        ],
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : _items.isEmpty
              ? Center(
                  child: Column(
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: [
                      Icon(Icons.delete_outline, size: 64, color: Colors.grey),
                      SizedBox(height: 16),
                      Text(t('回收站为空'), style: TextStyle(fontSize: 16, color: Colors.grey)),
                    ],
                  ),
                )
              : RefreshIndicator(
                  onRefresh: _loadRecycleList,
                  child: ListView.builder(
                    itemCount: _items.length,
                    itemBuilder: (context, index) {
                      final item = _items[index];
                      final isSelected = _selectedIds.contains(item.id);
                      return ListTile(
                        leading: Icon(
                          item.isDirectory ? Icons.folder : Icons.insert_drive_file,
                          color: isSelected ? Colors.blue : Colors.grey,
                        ),
                        title: Text(
                          item.originalName.isNotEmpty ? item.originalName : item.name,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                        subtitle: Text(
                          '${_formatSize(item.size)} · ${_formatDate(item.deletedAt)}',
                          style: const TextStyle(fontSize: 12),
                        ),
                        trailing: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            IconButton(
                              icon: const Icon(Icons.restore, color: Colors.green),
                              onPressed: () => _restoreItem(item.id, item.originalName),
                              tooltip: t('还原')),
                            IconButton(
                              icon: const Icon(Icons.delete_forever, color: Colors.red),
                              onPressed: () => _deletePermanently([item.id]),
                              tooltip: t('永久删除')),
                          ],
                        ),
                        onLongPress: () {
                          setState(() {
                            if (isSelected) {
                              _selectedIds.remove(item.id);
                            } else {
                              _selectedIds.add(item.id);
                            }
                          });
                        },
                        tileColor: isSelected ? Colors.blue.withOpacity(0.1) : null,
                      );
                    },
                  ),
                ),
    );
  }
}

class RecycleItem {
  final String id;
  final String name;
  final String originalPath;
  final String originalName;
  final String deletedAt;
  final int size;
  final bool isDirectory;

  RecycleItem({
    required this.id,
    required this.name,
    required this.originalPath,
    required this.originalName,
    required this.deletedAt,
    required this.size,
    required this.isDirectory,
  });

  factory RecycleItem.fromJson(Map<String, dynamic> json) {
    return RecycleItem(
      id: json['id'] ?? '',
      name: json['name'] ?? '',
      originalPath: json['original_path'] ?? '',
      originalName: json['original_name'] ?? '',
      deletedAt: json['deleted_at'] ?? '',
      size: json['size'] ?? 0,
      isDirectory: json['is_directory'] ?? false,
    );
  }
}
