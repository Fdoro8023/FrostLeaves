import 'dart:io';
import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:path_provider/path_provider.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';
import '../../../core/widgets/qr_view.dart';
import '../../../core/models/models.dart';
import '../../../core/widgets/floating_message.dart';

/// PC 服务端文件管理（需求 3）
/// 支持：点击进入目录、右键多选、批量下载/删除、为选中文件单独创建分享
class ServerFileManagerPage extends ConsumerStatefulWidget {
  const ServerFileManagerPage({super.key});
  @override
  ConsumerState<ServerFileManagerPage> createState() => _ServerFileManagerPageState();
}

class _ServerFileManagerPageState extends ConsumerState<ServerFileManagerPage> {
  String _currentPath = '/shared';
  List<FileItem> _items = [];
  bool _loading = true;
  bool _adminReady = false;
  bool _busy = false;
  String? _error;
  final Set<String> _selected = {};
  bool get _selectionMode => _selected.isNotEmpty;

  @override
  void initState() {
    super.initState();
    _init();
  }

  Future<void> _init() async {
    try {
      final api = ref.read(apiServiceProvider);
      final resp = await api.fetchAdminToken();
      final data = resp['data'] as Map<String, dynamic>?;
      final token = (data?['admin_token'] ?? '').toString();
      if (token.isEmpty) throw Exception(t(t('服务端未返回管理令牌')));
      api.setAdminToken(token);
      _adminReady = true;
      await _loadFiles();
    } catch (e) {
      setState(() {
        _loading = false;
        _error = t(t('无法建立管理通道：${ApiService.describeError(e)}（请确认运行在服务端这台机器上）'));
      });
    }
  }

  Future<void> _loadFiles() async {
    setState(() { _loading = true; _error = null; });
    try {
      final api = ref.read(apiServiceProvider);
      final resp = await api.listFiles(_currentPath);
      final items = (resp['items'] as List?)
              ?.map((e) => FileItem.fromJson(e as Map<String, dynamic>))
              .toList() ??
          [];
      if (!mounted) return;
      setState(() { _items = items; _loading = false; _selected.clear(); });
    } catch (e) {
      if (!mounted) return;
      setState(() { _loading = false; _items = []; _error = t(t('读取失败：${ApiService.describeError(e)}')); });
    }
  }

  void _navigateTo(String path) {
    _currentPath = path;
    _selected.clear();
    _loadFiles();
  }

  void _goUp() {
    final parts = _currentPath.split('/').where((p) => p.isNotEmpty).toList();
    if (parts.length > 1) {
      parts.removeLast();
      _navigateTo('/' + parts.join('/'));
    } else if (parts.length == 1) {
      _navigateTo('/');
    }
  }

  String _joinRel(String name) {
    final base = _currentPath.endsWith('/') ? _currentPath : '$_currentPath/';
    return '$base$name';
  }

  String _parentTarget() {
    final p = _currentPath.startsWith('/') ? _currentPath.substring(1) : _currentPath;
    return p.isEmpty ? 'shared' : p;
  }

  // ===== 选择 =====//
  void _toggle(String nameKey) {
    setState(() {
      if (_selected.contains(nameKey)) {
        _selected.remove(nameKey);
      } else {
        _selected.add(nameKey);
      }
    });
  }

  void _clearSelection() => setState(() => _selected.clear());

  void _selectAll() {
    setState(() {
      _selected
        ..clear()
        ..addAll(_items.where((i) => !i.isDirectory).map((i) => i.name));
    });
  }

  // ===== 批量操作 =====//
  /// 上传文件到当前目录（服务端模式：走回环管理员通道）
  Future<void> _uploadFiles() async {
    try {
      final result = await FilePicker.platform.pickFiles(allowMultiple: true);
      if (result == null || result.files.isEmpty) return;
      final api = ref.read(apiServiceProvider);
      var ok = 0;
      for (final f in result.files) {
        final local = f.path;
        if (local == null || local.isEmpty) continue;
        try {
          await api.uploadFile(local, _joinRel(f.name));
          ok++;
        } catch (_) {
          // 单个失败不影响其余
        }
      }
      if (!mounted) return;
      FloatingMessage.show(
        context: context,
        title: ok > 0 ? t(t('上传完成')) : t(t('上传失败')),
        message: ok > 0 ? t(t('已上传 $ok 个文件到 $_currentPath')) : t(t('请检查服务端状态后重试')),
        type: ok > 0 ? MessageType.success : MessageType.error,
      );
      await _loadFiles();
    } catch (e) {
      if (mounted) {
        FloatingMessage.show(context: context, title: t(t('上传失败')), message: '$e', type: MessageType.error);
      }
    }
  }

  Future<void> _batchDownload() async {
    if (_selected.isEmpty) return;
    setState(() => _busy = true);
    int ok = 0;
    final List<String> failed = [];
    try {
      Directory dir;
      try {
        dir = (await getDownloadsDirectory()) ?? await getApplicationDocumentsDirectory();
      } catch (_) {
        dir = await getApplicationDocumentsDirectory();
      }
      for (final name in _selected.toList()) {
        try {
          final resp = await ref.read(apiServiceProvider).downloadFile(_joinRel(name));
          final bytes = (resp.data as List).cast<int>();
          final out = File('${dir.path}${Platform.pathSeparator}$name');
          await out.writeAsBytes(bytes);
          ok++;
        } catch (e) {
          failed.add('$name: ${ApiService.describeError(e)}');
        }
      }
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(
        content: Text(failed.isEmpty
            ? t(t('已下载 $ok 个文件到 ${dir.path}'))
            : t(t('成功 $ok 个，失败 ${failed.length} 个：${failed.first}'))),
      ));
      _clearSelection();
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _batchDelete() async {
    if (_selected.isEmpty) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t(t('批量删除'))),
        content: Text(t(t('确定删除选中的 ${_selected.length} 个文件吗？文件会移入回收站，可从回收站恢复。'))),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: Text(t(t('取消')))),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(t(t('删除')), style: TextStyle(color: AppTheme.errorColor)),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    setState(() => _busy = true);
    try {
      final paths = _selected.map(_joinRel).toList();
      await ref.read(apiServiceProvider).deleteFiles(paths);
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t(t('已移入回收站：${paths.length} 个')))));
      _clearSelection();
      await _loadFiles();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t(t('删除失败：${ApiService.describeError(e)}')))));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _shareSelected() async {
    if (_selected.isEmpty) return;
    String perm = 'read';
    int hours = 24;
    final noteCtrl = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx2, setSt) => AlertDialog(
          title: Text(t(t('为选中的 ${_selected.length} 个文件创建分享'))),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(_selected.join('、'), maxLines: 3, overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 12)),
              SizedBox(height: 12),
              DropdownButtonFormField<String>(
                value: perm,
                decoration: InputDecoration(labelText: t(t('权限'))),
                items: [
                  DropdownMenuItem(value: 'read', child: Text(t(t('只读（可下载）')))),
                  DropdownMenuItem(value: 'upload', child: Text(t(t('只读 + 可上传')))),
                ],
                onChanged: (v) => setSt(() => perm = v ?? 'read'),
              ),
              SizedBox(height: 12),
              DropdownButtonFormField<int>(
                value: hours,
                decoration: InputDecoration(labelText: t(t('有效期'))),
                items: [
                  DropdownMenuItem(value: 1, child: Text(t(t('1 小时')))),
                  DropdownMenuItem(value: 24, child: Text(t(t('24 小时')))),
                  DropdownMenuItem(value: 168, child: Text(t(t('7 天')))),
                  DropdownMenuItem(value: -1, child: Text(t(t('永久')))),
                ],
                onChanged: (v) => setSt(() => hours = v ?? 24),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: noteCtrl,
                decoration: InputDecoration(labelText: t(t('备注（可选）'))),
              ),
            ],
          ),
          actions: [
            TextButton(onPressed: () => Navigator.pop(ctx2, false), child: Text(t(t('取消')))),
            ElevatedButton(onPressed: () => Navigator.pop(ctx2, true), child: Text(t(t('创建')))),
          ],
        ),
      ),
    );
    if (ok != true) return;
    setState(() => _busy = true);
    try {
      final resp = await ref.read(apiServiceProvider).createShare(
            target: _parentTarget(),
            perm: perm,
            expiresHours: hours,
            note: noteCtrl.text.trim(),
            files: _selected.toList(),
          );
      final data = resp['data'] as Map<String, dynamic>?;
      final lanUrl = (data?['lan_url'] ?? '').toString();
      final wanUrl = (data?['public_url'] ?? '').toString();
      if (!mounted) return;
      await showDialog<void>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: Text(t(t('分享已创建'))),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(t(t('只包含选中的 ${_selected.length} 个文件')),
                  style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
              const SizedBox(height: 12),
              _urlRow(t('内网访问'), lanUrl),
              if (wanUrl.isNotEmpty) ...[
                const SizedBox(height: 10),
                _urlRow(t('外网访问'), wanUrl),
              ],
              const SizedBox(height: 14),
              Center(
                child: QrView(
                  api: ref.read(apiServiceProvider),
                  text: wanUrl.isNotEmpty ? wanUrl : lanUrl,
                  size: 200,
                ),
              ),
              const SizedBox(height: 6),
              Center(
                child: Text(t('扫码打开'),
                    style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: () {
                Navigator.pop(ctx);
                setState(() => _selected.clear());
              },
              child: Text(t(t('关闭'))),
            ),
          ],
        ),
      );
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t(t('创建分享失败：${ApiService.describeError(e)}')))));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _renameItem(FileItem item) async {
    final controller = TextEditingController(text: item.name);
    final newName = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t(t('重命名'))),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: InputDecoration(labelText: t(t('新名称'))),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: Text(t(t('取消')))),
          ElevatedButton(
            onPressed: () => Navigator.pop(ctx, controller.text.trim()),
            child: Text(t(t('确定'))),
          ),
        ],
      ),
    );
    if (newName == null || newName.isEmpty || newName == item.name) return;
    try {
      final api = ref.read(apiServiceProvider);
      await api.renameFile(_joinRel(item.name), _joinRel(newName));
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t(t('重命名成功')))));
      _loadFiles();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t(t('重命名失败：${ApiService.describeError(e)}')))));
    }
  }

  Future<void> _deleteItem(FileItem item) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t(t('确认删除'))),
        content: Text(t(t('确定删除「${item.name}」吗？文件会移入回收站，可从回收站恢复。'))),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: Text(t(t('取消')))),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(t(t('删除')), style: TextStyle(color: AppTheme.errorColor)),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    try {
      await ref.read(apiServiceProvider).deleteFiles([_joinRel(item.name)]);
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t(t('已移入回收站')))));
      _loadFiles();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t(t('删除失败：${ApiService.describeError(e)}')))));
    }
  }

  /// 分享地址行：地址 + 一键复制
  Widget _urlRow(String label, String url) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(label, style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
        const SizedBox(height: 4),
        Row(
          children: [
            Expanded(
              child: SelectableText(url.isEmpty ? '-' : url,
                  style: TextStyle(fontSize: 12, color: AppTheme.primaryColor)),
            ),
            IconButton(
              icon: const Icon(Icons.copy, size: 16),
              tooltip: t(t('复制')),
              onPressed: url.isEmpty
                  ? null
                  : () {
                      Clipboard.setData(ClipboardData(text: url));
                      ScaffoldMessenger.of(context).showSnackBar(
                          SnackBar(content: Text(t(t('已复制$label地址')))));
                    },
            ),
          ],
        ),
      ],
    );
  }

  /// 右键菜单（PC）
  Future<void> _showContextMenu(FileItem item, Offset pos) async {
    final isDir = item.isDirectory;
    final choice = await showMenu<String>(
      context: context,
      position: RelativeRect.fromLTRB(pos.dx, pos.dy, pos.dx, pos.dy),
      items: [
        if (!isDir) PopupMenuItem(value: 'select', child: Text(t(t('多选（勾选）')))),
        if (!isDir) PopupMenuItem(value: 'download', child: Text(t(t('下载')))),
        PopupMenuItem(value: 'share', child: Text(t(t('创建分享')))),
        PopupMenuItem(value: 'rename', child: Text(t(t('重命名')))),
        PopupMenuItem(value: 'delete', child: Text(t(t('删除')))),
      ],
    );
    if (!mounted) return;
    switch (choice) {
      case 'select':
        _toggle(item.name);
        break;
      case 'download':
        setState(() => _busy = true);
        try {
          final resp = await ref.read(apiServiceProvider).downloadFile(_joinRel(item.name));
          final bytes = (resp.data as List).cast<int>();
          Directory dir;
          try {
            dir = (await getDownloadsDirectory()) ?? await getApplicationDocumentsDirectory();
          } catch (_) {
            dir = await getApplicationDocumentsDirectory();
          }
          await File('${dir.path}${Platform.pathSeparator}${item.name}').writeAsBytes(bytes);
          if (mounted) {
            ScaffoldMessenger.of(context).showSnackBar(
                SnackBar(content: Text(t(t('已下载到 ${dir.path}')))));
          }
        } catch (e) {
          if (mounted) {
            ScaffoldMessenger.of(context).showSnackBar(
                SnackBar(content: Text(t(t('下载失败：${ApiService.describeError(e)}')))));
          }
        } finally {
          if (mounted) setState(() => _busy = false);
        }
        break;
      case 'share':
        setState(() => _selected
          ..clear()
          ..add(item.name));
        await _shareSelected();
        break;
      case 'rename':
        _renameItem(item);
        break;
      case 'delete':
        _deleteItem(item);
        break;
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
              Text(_selectionMode ? t(t('已选 ${_selected.length} 个文件')) : t(t('文件管理（公共文件）')),
                  style: TextStyle(fontSize: 24, fontWeight: FontWeight.bold, color: AppTheme.textPrimary)),
              const SizedBox(width: 12),
              if (!_adminReady)
                Text(t(t('管理通道未就绪')), style: TextStyle(fontSize: 12, color: AppTheme.errorColor)),
              const Spacer(),
              if (_selectionMode) ...[
                TextButton(onPressed: _selectAll, child: Text(t(t('全选')))),
                TextButton(onPressed: _clearSelection, child: Text(t(t('取消选择')))),
              ],
              IconButton(onPressed: _loadFiles, icon: const Icon(Icons.refresh), tooltip: t(t('刷新'))),
          IconButton(
            onPressed: _busy ? null : _uploadFiles,
            icon: const Icon(Icons.upload_file),
            tooltip: t(t('上传到此目录')),
          ),
            ],
          ),
          const SizedBox(height: 8),
          Text(_selectionMode ? t(t('批量操作：下载 / 删除 / 创建分享（只含选中文件）')) : t(t('右键文件可多选、批量下载/删除、或为选中文件创建分享')),
              style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
          const SizedBox(height: 12),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
            decoration: BoxDecoration(
              color: AppTheme.surfaceColor,
              borderRadius: BorderRadius.circular(8),
              border: Border.all(color: AppTheme.borderColor),
            ),
            child: Row(
              children: [
                IconButton(icon: const Icon(Icons.arrow_upward, size: 18), onPressed: _goUp, color: AppTheme.textSecondary),
                const SizedBox(width: 8),
                Expanded(child: Text(_currentPath, style: TextStyle(color: AppTheme.textPrimary, fontSize: 14))),
                TextButton.icon(onPressed: () => _navigateTo('/shared'), icon: const Icon(Icons.folder, size: 16), label: Text(t(t('共享')))),
                TextButton.icon(onPressed: () => _navigateTo('/public'), icon: const Icon(Icons.public, size: 16), label: Text(t(t('公共')))),
              ],
            ),
          ),
          const SizedBox(height: 16),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: Text(_error!, style: TextStyle(fontSize: 13, color: AppTheme.errorColor)),
            ),
          Expanded(
            child: _loading
                ? const Center(child: CircularProgressIndicator())
                : _items.isEmpty
                    ? Center(child: Text(t(t('空目录')), style: TextStyle(color: AppTheme.textSecondary)))
                    : ListView.builder(
                        itemCount: _items.length,
                        itemBuilder: (ctx, i) {
                          final item = _items[i];
                          final selected = _selected.contains(item.name);
                          return GestureDetector(
                            onSecondaryTapDown: (d) => _showContextMenu(item, d.globalPosition),
                            child: ListTile(
                              selected: selected,
                              selectedTileColor: AppTheme.primaryColor.withOpacity(0.08),
                              leading: _selectionMode && !item.isDirectory
                                  ? Checkbox(
                                      value: selected,
                                      onChanged: (_) => _toggle(item.name),
                                    )
                                  : Icon(
                                      item.isDirectory ? Icons.folder : Icons.insert_drive_file,
                                      color: item.isDirectory ? AppTheme.warningColor : AppTheme.textSecondary,
                                    ),
                              title: Text(item.name, style: TextStyle(color: AppTheme.textPrimary)),
                              subtitle: Text(
                                item.isDirectory ? t(t('目录')) : '${item.formattedSize} · ${item.modified}',
                                style: TextStyle(fontSize: 12, color: AppTheme.textSecondary),
                              ),
                              trailing: Row(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  if (item.isDirectory)
                                    IconButton(
                                      icon: const Icon(Icons.arrow_forward_ios, size: 16),
                                      tooltip: t(t('打开')),
                                      onPressed: () => _navigateTo(_joinRel(item.name)),
                                    ),
                                  IconButton(
                                    icon: const Icon(Icons.drive_file_rename_outline, size: 18),
                                    tooltip: t(t('重命名')),
                                    onPressed: () => _renameItem(item),
                                  ),
                                  IconButton(
                                    icon: Icon(Icons.delete_outline, size: 18, color: AppTheme.errorColor),
                                    tooltip: t(t('删除')),
                                    onPressed: () => _deleteItem(item),
                                  ),
                                ],
                              ),
                            ),
                          );
                        },
                      ),
          ),
          if (_selectionMode)
            Container(
              padding: const EdgeInsets.only(top: 12),
              child: Row(
                children: [
                  ElevatedButton.icon(
                    onPressed: _busy ? null : _batchDownload,
                    icon: const Icon(Icons.download, size: 18),
                    label: Text(t(t('批量下载 (${_selected.length})'))),
                  ),
                  const SizedBox(width: 12),
                  ElevatedButton.icon(
                    onPressed: _busy ? null : _shareSelected,
                    icon: const Icon(Icons.share, size: 18),
                    label: Text(t(t('创建分享'))),
                  ),
                  const SizedBox(width: 12),
                  OutlinedButton.icon(
                    onPressed: _busy ? null : _batchDelete,
                    icon: const Icon(Icons.delete, size: 18),
                    label: Text(t(t('批量删除'))),
                    style: OutlinedButton.styleFrom(
                      foregroundColor: AppTheme.errorColor,
                      side: BorderSide(color: AppTheme.errorColor),
                    ),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}
