import 'dart:io';
import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:file_picker/file_picker.dart';
import 'package:path_provider/path_provider.dart';
import 'package:open_file/open_file.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';
import '../../../core/models/models.dart';
import '../../../core/widgets/floating_message.dart';
import 'recycle_bin_page.dart';

class ClientFileBrowserPage extends ConsumerStatefulWidget {
  const ClientFileBrowserPage({super.key});
  @override
  ConsumerState<ClientFileBrowserPage> createState() => _ClientFileBrowserPageState();
}

class _ClientFileBrowserPageState extends ConsumerState<ClientFileBrowserPage> {
  String _currentPath = '/shared';
  List<FileItem> _items = [];
  bool _loading = true;
  final Set<String> _selected = {};
  bool _busy = false; // 需求①：批量操作中
  bool get _selectionMode => _selected.isNotEmpty;

  void _toggle(String name) {
    setState(() {
      if (_selected.contains(name)) {
        _selected.remove(name);
      } else {
        _selected.add(name);
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

  String _joinRel(String name) => '$_currentPath/$name';

  String _parentTarget() {
    final p = _currentPath.startsWith('/') ? _currentPath.substring(1) : _currentPath;
    return p.isEmpty ? 'shared' : p;
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
              tooltip: t('复制'),
              onPressed: url.isEmpty
                  ? null
                  : () {
                      Clipboard.setData(ClipboardData(text: url));
                      ScaffoldMessenger.of(context).showSnackBar(
                          SnackBar(content: Text(t('已复制$label地址'))));
                    },
            ),
          ],
        ),
      ],
    );
  }

  /// 需求①：下载单个文件到本机（复用 _downloadFile 的保存位置策略）
  Future<void> _downloadOne(String name) async {
    final api = ref.read(apiServiceProvider);
    final resp = await api.downloadFile(_joinRel(name));
    Directory? dir;
    if (Platform.isAndroid) {
      dir = await getExternalStorageDirectory();
    } else {
      dir = await getApplicationDocumentsDirectory();
    }
    if (dir == null) throw Exception(t('无法获取存储目录'));
    await File('${dir.path}/$name').writeAsBytes((resp.data as List).cast<int>());
  }

  /// 需求①：批量下载（逐个）
  Future<void> _batchDownload() async {
    if (_selected.isEmpty) return;
    setState(() => _busy = true);
    int ok = 0;
    final failed = <String>[];
    try {
      for (final name in _selected.toList()) {
        try {
          await _downloadOne(name);
          ok++;
        } catch (_) {
          failed.add(name);
        }
      }
      if (!mounted) return;
      FloatingMessage.show(
        context: context,
        title: failed.isEmpty ? t('批量下载完成') : t('部分失败'),
        message: failed.isEmpty ? t('已下载 $ok 个文件') : t('成功 $ok 个，失败 ${failed.length} 个'),
        type: failed.isEmpty ? MessageType.success : MessageType.error,
      );
      _clearSelection();
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// 需求①：批量删除
  Future<void> _batchDelete() async {
    if (_selected.isEmpty) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t('批量删除')),
        content: Text(t('确定删除选中的 ${_selected.length} 个文件吗？')),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: Text(t('取消'))),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(t('删除'), style: TextStyle(color: Colors.red)),
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
      FloatingMessage.show(
        context: context,
        title: t('已删除'),
        message: t('${paths.length} 个文件已移入回收站'),
        type: MessageType.success,
      );
      _clearSelection();
      await _loadFiles();
    } catch (e) {
      if (!mounted) return;
      FloatingMessage.show(
        context: context,
        title: t('删除失败'),
        message: ApiService.describeError(e),
        type: MessageType.error,
      );
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// 需求①：为选中文件创建分享（只含这几个文件）
  Future<void> _shareSelected() async {
    if (_selected.isEmpty) return;
    String perm = 'read';
    int hours = 24;
    final noteCtrl = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx2, setSt) => AlertDialog(
          title: Text(t('分享选中的 ${_selected.length} 个文件')),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(_selected.join('、'), maxLines: 3, overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 12)),
              SizedBox(height: 12),
              DropdownButtonFormField<String>(
                value: perm,
                decoration: InputDecoration(labelText: t('权限')),
                items: [
                  DropdownMenuItem(value: 'read', child: Text(t('只读（可下载）'))),
                  DropdownMenuItem(value: 'upload', child: Text(t('只读 + 可上传'))),
                ],
                onChanged: (v) => setSt(() => perm = v ?? 'read'),
              ),
              SizedBox(height: 12),
              DropdownButtonFormField<int>(
                value: hours,
                decoration: InputDecoration(labelText: t('有效期')),
                items: [
                  DropdownMenuItem(value: 1, child: Text(t('1 小时'))),
                  DropdownMenuItem(value: 24, child: Text(t('24 小时'))),
                  DropdownMenuItem(value: 168, child: Text(t('7 天'))),
                  DropdownMenuItem(value: -1, child: Text(t('永久'))),
                ],
                onChanged: (v) => setSt(() => hours = v ?? 24),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: noteCtrl,
                decoration: InputDecoration(labelText: t('备注（可选）')),
              ),
            ],
          ),
          actions: [
            TextButton(onPressed: () => Navigator.pop(ctx2, false), child: Text(t('取消'))),
            ElevatedButton(onPressed: () => Navigator.pop(ctx2, true), child: Text(t('创建'))),
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
          title: Text(t('分享已创建')),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(t('只包含选中的 ${_selected.length} 个文件'),
                  style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
              const SizedBox(height: 12),
              _urlRow(t('内网访问'), lanUrl),
              if (wanUrl.isNotEmpty) ...[
                const SizedBox(height: 10),
                _urlRow(t('外网访问'), wanUrl),
              ],
            ],
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx),
              child: Text(t('关闭')),
            ),
          ],
        ),
      );
      _clearSelection();
    } catch (e) {
      if (!mounted) return;
      FloatingMessage.show(
        context: context,
        title: t('创建分享失败'),
        message: ApiService.describeError(e),
        type: MessageType.error,
      );
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }
  
  // Upload task management
  final List<Map<String, dynamic>> _uploadTasks = [];
  int _taskCounter = 0;

  @override
  void initState() {
    super.initState();
    _loadFiles();
  }

  Future<void> _loadFiles() async {
    setState(() { _loading = true; _selected.clear(); });
    try {
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();
      final resp = await api.listFiles(_currentPath);
      final items = (resp['items'] as List?)?.map((e) => FileItem.fromJson(e as Map<String, dynamic>)).toList() ?? [];
      setState(() { _items = items; _loading = false; });
    } catch (e) {
      setState(() => _loading = false);
      if (mounted) {
        FloatingMessage.show(
          context: context,
          title: t('加载失败'),
          message: e.toString(),
          type: MessageType.error,
        );
      }
    }
  }

  void _navigateTo(String path) {
    _currentPath = path;
    _loadFiles();
  }

  void _goUp() {
    final parts = _currentPath.split('/');
    if (parts.length > 1) {
      parts.removeLast();
      _navigateTo(parts.join('/') == '' ? '/' : parts.join('/'));
    }
  }

  Future<void> _createFolder() async {
    final controller = TextEditingController();
    final result = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t('新建文件夹')),
        content: TextField(
          controller: controller,
          decoration: InputDecoration(
            hintText: t('请输入文件夹名称'),
            border: OutlineInputBorder(),
          ),
          autofocus: true,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text(t('取消')),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, controller.text.trim()),
            child: Text(t('创建')),
          ),
        ],
      ),
    );

    if (result == null || result.isEmpty) return;

    try {
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();
      final folderPath = '$_currentPath/$result';
      await api.createDirectory(folderPath);
      _loadFiles();
      
      if (mounted) {
        FloatingMessage.show(
          context: context,
          title: t('创建成功'),
          message: t('文件夹 $result 已创建'),
          type: MessageType.success,
        );
      }
    } catch (e) {
      if (mounted) {
        FloatingMessage.show(
          context: context,
          title: t('创建失败'),
          message: e.toString(),
          type: MessageType.error,
        );
      }
    }
  }

  Future<void> _renameItem(String oldName) async {
    final controller = TextEditingController(text: oldName);
    final result = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t('重命名')),
        content: TextField(
          controller: controller,
          decoration: InputDecoration(
            hintText: t('请输入新名称'),
            border: OutlineInputBorder(),
          ),
          autofocus: true,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text(t('取消')),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, controller.text.trim()),
            child: Text(t('确定')),
          ),
        ],
      ),
    );

    if (result == null || result.isEmpty || result == oldName) return;

    try {
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();
      final oldPath = '$_currentPath/$oldName';
      final newPath = '$_currentPath/$result';
      await api.renameFile(oldPath, newPath);
      _loadFiles();
      
      if (mounted) {
        FloatingMessage.show(
          context: context,
          title: t('重命名成功'),
          message: '$oldName → $result',
          type: MessageType.success,
        );
      }
    } catch (e) {
      if (mounted) {
        FloatingMessage.show(
          context: context,
          title: t('重命名失败'),
          message: e.toString(),
          type: MessageType.error,
        );
      }
    }
  }

  Future<void> _uploadFile() async {
    try {
      final result = await FilePicker.platform.pickFiles();
      if (result == null || result.files.isEmpty) return;

      final file = result.files.first;
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();
      
      final remotePath = '$_currentPath/${file.name}';
      final taskId = ++_taskCounter;
      
      // Add task to list
      setState(() {
        _uploadTasks.add({
          'id': taskId,
          'type': 'upload',
          'fileName': file.name,
          'progress': 0.0,
          'speed': 0,
          'size': file.size,
          'status': 'uploading',
          'startTime': DateTime.now(),
        });
      });

      try {
        await api.uploadFile(
          file.path!,
          remotePath,
          onProgress: (sent, total) {
            if (mounted) {
              final progress = total > 0 ? sent / total : 0.0;
              final elapsed = DateTime.now().difference(
                _uploadTasks.firstWhere((t) => t['id'] == taskId)['startTime']
              ).inSeconds;
              final speed = elapsed > 0 ? sent ~/ elapsed : 0;
              
              setState(() {
                final task = _uploadTasks.firstWhere((t) => t['id'] == taskId);
                task['progress'] = progress;
                task['speed'] = speed;
              });
            }
          },
        );
        
        // Mark as completed
        setState(() {
          final task = _uploadTasks.firstWhere((t) => t['id'] == taskId);
          task['status'] = 'completed';
          task['progress'] = 1.0;
        });
        
        _loadFiles();
        
        if (mounted) {
          FloatingMessage.show(
            context: context,
            title: t('上传成功'),
            message: t('文件 ${file.name} 已上传'),
            type: MessageType.success,
          );
        }
      } catch (e) {
        setState(() {
          final task = _uploadTasks.firstWhere((t) => t['id'] == taskId);
          task['status'] = 'failed';
        });
        
        if (mounted) {
          FloatingMessage.show(
            context: context,
            title: t('上传失败'),
            message: e.toString(),
            type: MessageType.error,
          );
        }
      }
    } catch (e) {
      if (mounted) {
        FloatingMessage.show(
          context: context,
          title: t('选择文件失败'),
          message: e.toString(),
          type: MessageType.error,
        );
      }
    }
  }

  void _removeTask(int taskId) {
    setState(() {
      _uploadTasks.removeWhere((t) => t['id'] == taskId);
    });
  }

  Future<void> _downloadFile(String name) async {
    final taskId = ++_taskCounter;
    final startTime = DateTime.now();
    
    // Add task to list
    setState(() {
      _uploadTasks.add({
        'id': taskId,
        'type': 'download',
        'fileName': name,
        'progress': 0.0,
        'speed': 0,
        'size': 0,
        'status': 'downloading',
        'startTime': startTime,
      });
    });
    
    try {
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();
      
      final remotePath = '$_currentPath/$name';
      print('Downloading: $remotePath');
      
      final resp = await api.downloadFile(
        remotePath,
        onProgress: (received, total) {
          if (mounted) {
            final progress = total > 0 ? received / total : 0.0;
            final elapsed = DateTime.now().difference(startTime).inSeconds;
            final speed = elapsed > 0 ? received ~/ elapsed : 0;
            
            setState(() {
              final task = _uploadTasks.firstWhere((t) => t['id'] == taskId);
              task['progress'] = progress;
              task['speed'] = speed;
              task['size'] = total;
            });
          }
        },
      );
      print('Downloaded ${resp.data.length} bytes');
      
      // Save to app's own directory (avoids permission issues)
      Directory? dir;
      if (Platform.isAndroid) {
        dir = await getExternalStorageDirectory();
      } else {
        dir = await getApplicationDocumentsDirectory();
      }
      
      if (dir == null) {
        throw Exception(t('无法获取存储目录'));
      }
      
      final savePath = '${dir?.path}/$name';
      print('Saving to: $savePath');
      
      final file = File(savePath);
      await file.writeAsBytes(resp.data);
      print('File saved successfully');
      
      // Mark as completed
      setState(() {
        final task = _uploadTasks.firstWhere((t) => t['id'] == taskId);
        task['status'] = 'completed';
        task['progress'] = 1.0;
      });
      
      if (mounted) {
        FloatingMessage.show(
          context: context,
          title: t('下载成功'),
          message: t('已保存到: $savePath'),
          type: MessageType.success,
        );
        
        // Try to open the file
        try {
          await OpenFile.open(savePath);
        } catch (e) {
          print('Failed to open file: $e');
        }
      }
    } catch (e) {
      print('Download error: $e');
      setState(() {
        final task = _uploadTasks.firstWhere((t) => t['id'] == taskId);
        task['status'] = 'failed';
      });
      if (mounted) {
        FloatingMessage.show(
          context: context,
          title: t('下载失败'),
          message: e.toString(),
          type: MessageType.error,
        );
      }
    }
  }

  Future<void> _previewFile(String name) async {
    try {
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();
      
      final remotePath = '$_currentPath/$name';
      final resp = await api.downloadFile(remotePath);
      
      // Save to temp directory
      final tempDir = await getTemporaryDirectory();
      final tempPath = '${tempDir.path}/$name';
      final file = File(tempPath);
      await file.writeAsBytes(resp.data);
      
      // Open file
      final result = await OpenFile.open(tempPath);
      if (result.type != ResultType.done) {
        if (mounted) {
          FloatingMessage.show(
            context: context,
            title: t('无法打开文件'),
            message: result.message ?? t('未知错误'),
            type: MessageType.warning,
          );
        }
      }
    } catch (e) {
      if (mounted) {
        FloatingMessage.show(
          context: context,
          title: t('预览失败'),
          message: e.toString(),
          type: MessageType.error,
        );
      }
    }
  }

  Future<void> _deleteFile(String name) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t('确认删除')),
        content: Text(t('确定要删除 $name 吗？文件将移入回收站，30天后自动清除。')),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: Text(t('取消'))),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(t('删除'), style: TextStyle(color: AppTheme.errorColor)),
          ),
        ],
      ),
    );

    if (confirmed != true) return;

    try {
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();
      await api.deleteFiles(['$_currentPath/$name']);
      _loadFiles();
      
      if (mounted) {
        FloatingMessage.show(
          context: context,
          title: t('已移入回收站'),
          message: t('文件 $name 已移入回收站，可在回收站中还原'),
          type: MessageType.success,
        );
      }
    } catch (e) {
      if (mounted) {
        FloatingMessage.show(
          context: context,
          title: t('删除失败'),
          message: e.toString(),
          type: MessageType.error,
        );
      }
    }
  }

  IconData _fileIcon(String name) {
    final ext = name.split('.').last.toLowerCase();
    switch (ext) {
      case 'pdf': return Icons.picture_as_pdf;
      case 'jpg': case 'jpeg': case 'png': case 'gif': return Icons.image;
      case 'mp4': case 'avi': case 'mkv': return Icons.video_file;
      case 'mp3': case 'wav': case 'flac': return Icons.audio_file;
      case 'zip': case 'rar': case '7z': return Icons.archive;
      case 'doc': case 'docx': return Icons.description;
      case 'xls': case 'xlsx': return Icons.table_chart;
      default: return Icons.insert_drive_file;
    }
  }

  String _formatSpeed(int bytesPerSec) {
    if (bytesPerSec < 1024) return '$bytesPerSec B/s';
    if (bytesPerSec < 1024 * 1024) return '${(bytesPerSec / 1024).toStringAsFixed(1)} KB/s';
    return '${(bytesPerSec / (1024 * 1024)).toStringAsFixed(1)} MB/s';
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        // Path bar
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          decoration: BoxDecoration(
            color: AppTheme.surfaceColor,
            border: Border(bottom: BorderSide(color: AppTheme.borderColor)),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              // Path display
              Row(
                children: [
                  IconButton(
                    icon: const Icon(Icons.arrow_upward, size: 18),
                    onPressed: _goUp,
                    tooltip: t('上级目录'),
                    padding: EdgeInsets.zero,
                    constraints: const BoxConstraints(minWidth: 32, minHeight: 32),
                  ),
                  Expanded(
                    child: Text(
                      _currentPath,
                      style: TextStyle(color: AppTheme.textPrimary, fontSize: 13),
                      overflow: TextOverflow.ellipsis,
                      maxLines: 1,
                    ),
                  ),
                  IconButton(
                    icon: const Icon(Icons.refresh, size: 18),
                    onPressed: _loadFiles,
                    tooltip: t('刷新'),
                    padding: EdgeInsets.zero,
                    constraints: const BoxConstraints(minWidth: 32, minHeight: 32),
                  ),
                ],
              ),
              if (_selectionMode)
                Row(
                  mainAxisAlignment: MainAxisAlignment.spaceEvenly,
                  children: [
                    _ActionButton(icon: Icons.download_outlined, label: t('下载${_selected.length}'), onTap: _busy ? () {} : _batchDownload),
                    _ActionButton(icon: Icons.share_outlined, label: t('分享'), onTap: _busy ? () {} : _shareSelected),
                    _ActionButton(icon: Icons.delete_outline, label: t('删除'), onTap: _busy ? () {} : _batchDelete),
                    _ActionButton(icon: Icons.select_all, label: t('全选'), onTap: _selectAll),
                    _ActionButton(icon: Icons.close, label: t('取消'), onTap: _clearSelection),
                  ],
                ),
              const SizedBox(height: 4),
              // Action buttons
              Row(
                mainAxisAlignment: MainAxisAlignment.spaceEvenly,
                children: [
                  _ActionButton(icon: Icons.folder_outlined, label: t('共享'), onTap: () => _navigateTo('/shared')),
                  _ActionButton(icon: Icons.person_outlined, label: t('私人'), onTap: () => _navigateTo('/private')),
                  _ActionButton(icon: Icons.create_new_folder_outlined, label: t('新建'), onTap: _createFolder),
                  _ActionButton(icon: Icons.upload_outlined, label: t('上传'), onTap: _uploadFile),
                  _ActionButton(icon: Icons.delete_outline, label: t('回收站'), onTap: () {
                    Navigator.push(
                      context,
                      MaterialPageRoute(builder: (context) => const RecycleBinPage()),
                    );
                  }),
                ],
              ),
            ],
          ),
        ),
        // Upload tasks
        if (_uploadTasks.isNotEmpty)
          Container(
            constraints: const BoxConstraints(maxHeight: 200),
            decoration: BoxDecoration(
              color: AppTheme.cardColor,
              border: Border(bottom: BorderSide(color: AppTheme.borderColor)),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                  child: Row(
                    children: [
                      Text(
                        t('传输任务'),
                        style: TextStyle(
                          fontSize: 13,
                          fontWeight: FontWeight.w600,
                          color: AppTheme.textPrimary,
                        ),
                      ),
                      const Spacer(),
                      TextButton(
                        onPressed: () => setState(() => _uploadTasks.clear()),
                        child: Text(t('清空'), style: TextStyle(fontSize: 12)),
                      ),
                    ],
                  ),
                ),
                Expanded(
                  child: ListView(
                    children: _uploadTasks.map((task) => _buildTaskRow(task)).toList(),
                  ),
                ),
              ],
            ),
          ),
        // File list
        Expanded(
          child: _loading
              ? const Center(child: CircularProgressIndicator())
              : _items.isEmpty
                  ? Center(
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Icon(Icons.folder_off_outlined, size: 48, color: AppTheme.textSecondary),
                          const SizedBox(height: 8),
                          Text(t('空目录'), style: TextStyle(color: AppTheme.textSecondary)),
                          const SizedBox(height: 16),
                          ElevatedButton.icon(
                            onPressed: _uploadFile,
                            icon: const Icon(Icons.upload),
                            label: Text(t('上传文件')),
                          ),
                        ],
                      ),
                    )
                  : ListView.builder(
                      itemCount: _items.length,
                      itemBuilder: (ctx, i) {
                        final item = _items[i];
                        final isSelected = _selected.contains(item.name);
                        return ListTile(
                          leading: _selectionMode && !item.isDirectory
                              ? Checkbox(value: isSelected, onChanged: (_) => _toggle(item.name))
                              : Icon(
                            item.isDirectory ? Icons.folder : _fileIcon(item.name),
                            color: item.isDirectory ? AppTheme.warningColor : AppTheme.textSecondary,
                            size: 24,
                          ),
                          title: Text(
                            item.name,
                            style: TextStyle(
                              color: AppTheme.textPrimary,
                              fontWeight: isSelected ? FontWeight.w600 : null,
                            ),
                          ),
                          subtitle: Text(
                            item.isDirectory ? t('目录') : item.formattedSize,
                            style: TextStyle(fontSize: 12, color: AppTheme.textSecondary),
                          ),
                          selected: isSelected,
                          selectedTileColor: AppTheme.primaryColor.withOpacity(0.1),
                          onTap: () {
                            if (item.isDirectory) {
                              _navigateTo('$_currentPath/${item.name}');
                            } else {
                              _previewFile(item.name);
                            }
                          },
                          onLongPress: () {
                            if (!item.isDirectory) {
                              _toggle(item.name);
                            } else if (_currentPath.startsWith('/private')) {
                              _showItemOptions(item.name, item.isDirectory);
                            }
                          },
                          trailing: Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              if (!item.isDirectory)
                                IconButton(
                                  icon: const Icon(Icons.download_outlined, size: 18),
                                  onPressed: () => _downloadFile(item.name),
                                  tooltip: t('下载'),
                                ),
                              PopupMenuButton<String>(
                                icon: const Icon(Icons.more_vert, size: 18),
                                onSelected: (value) {
                                  if (value == 'select') {
                                    _toggle(item.name);
                                  } else if (value == 'share') {
                                    setState(() {
                                      _selected
                                        ..clear()
                                        ..add(item.name);
                                    });
                                    _shareSelected();
                                  } else if (value == 'rename') {
                                    _renameItem(item.name);
                                  } else if (value == 'delete') {
                                    _deleteFile(item.name);
                                  }
                                },
                                itemBuilder: (ctx) => [
                                  if (!item.isDirectory) PopupMenuItem(value: 'select', child: Text(t('多选'))),
                                  PopupMenuItem(value: 'share', child: Text(t('创建分享'))),
                                  PopupMenuItem(value: 'rename', child: Text(t('重命名'))),
                                  PopupMenuItem(value: 'delete', child: Text(t('删除'), style: TextStyle(color: Colors.red))),
                                ],
                              ),
                            ],
                          ),
                        );
                      },
                    ),
        ),
      ],
    );
  }

  void _showItemOptions(String name, bool isDirectory) {
    showModalBottomSheet(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.edit),
              title: Text(t('重命名')),
              onTap: () {
                Navigator.pop(ctx);
                _renameItem(name);
              },
            ),
            if (!isDirectory)
              ListTile(
                leading: const Icon(Icons.download),
                title: Text(t('下载')),
                onTap: () {
                  Navigator.pop(ctx);
                  _downloadFile(name);
                },
              ),
            ListTile(
              leading: const Icon(Icons.delete, color: Colors.red),
              title: Text(t('删除'), style: TextStyle(color: Colors.red)),
              onTap: () {
                Navigator.pop(ctx);
                _deleteFile(name);
              },
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildTaskRow(Map<String, dynamic> task) {
    final id = task['id'] as int;
    final type = task['type'] as String;
    final fileName = task['fileName'] as String;
    final progress = task['progress'] as double;
    final speed = task['speed'] as int;
    final size = task['size'] as int;
    final status = task['status'] as String;
    final startTime = task['startTime'] as DateTime;

    String statusText;
    Color statusColor;
    IconData statusIcon;
    
    switch (status) {
      case 'uploading':
      case 'downloading':
        statusText = t('进行中');
        statusColor = AppTheme.primaryColor;
        statusIcon = Icons.sync;
        break;
      case 'completed':
        statusText = t('已完成');
        statusColor = AppTheme.successColor;
        statusIcon = Icons.check_circle;
        break;
      case 'failed':
        statusText = t('失败');
        statusColor = AppTheme.errorColor;
        statusIcon = Icons.error;
        break;
      case 'paused':
        statusText = t('已暂停');
        statusColor = AppTheme.warningColor;
        statusIcon = Icons.pause;
        break;
      default:
        statusText = status;
        statusColor = AppTheme.textSecondary;
        statusIcon = Icons.info;
    }

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      decoration: BoxDecoration(
        border: Border(
          bottom: BorderSide(color: AppTheme.borderColor, width: 0.5),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(statusIcon, size: 16, color: statusColor),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  '#$id $fileName',
                  style: TextStyle(
                    fontSize: 12,
                    color: AppTheme.textPrimary,
                  ),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              Text(
                statusText,
                style: TextStyle(
                  fontSize: 11,
                  color: statusColor,
                  fontWeight: FontWeight.w500,
                ),
              ),
              if (status == 'uploading' || status == 'downloading') ...[
                const SizedBox(width: 8),
                IconButton(
                  icon: const Icon(Icons.close, size: 16),
                  padding: EdgeInsets.zero,
                  constraints: const BoxConstraints(),
                  onPressed: () => _removeTask(id),
                ),
              ],
            ],
          ),
          const SizedBox(height: 4),
          Row(
            children: [
              Text(
                '${(progress * 100).toStringAsFixed(1)}%',
                style: TextStyle(fontSize: 11, color: AppTheme.textSecondary),
              ),
              const SizedBox(width: 12),
              Text(
                _formatSpeed(speed),
                style: TextStyle(fontSize: 11, color: AppTheme.textSecondary),
              ),
              const Spacer(),
              Text(
                '${(size * progress / (1024 * 1024)).toStringAsFixed(1)} / ${(size / (1024 * 1024)).toStringAsFixed(1)} MB',
                style: TextStyle(fontSize: 11, color: AppTheme.textSecondary),
              ),
            ],
          ),
          const SizedBox(height: 4),
          ClipRRect(
            borderRadius: BorderRadius.circular(2),
            child: LinearProgressIndicator(
              value: progress,
              backgroundColor: AppTheme.borderColor,
              valueColor: AlwaysStoppedAnimation<Color>(statusColor),
              minHeight: 3,
            ),
          ),
        ],
      ),
    );
  }
}

class _ActionButton extends StatelessWidget {
  final IconData icon;
  final String label;
  final VoidCallback onTap;

  const _ActionButton({
    required this.icon,
    required this.label,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(8),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 18, color: AppTheme.textSecondary),
            const SizedBox(height: 2),
            Text(
              label,
              style: TextStyle(
                fontSize: 10,
                color: AppTheme.textSecondary,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
