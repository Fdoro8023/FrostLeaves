import 'dart:io';

import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';
import '../../../core/widgets/theme_mode_selector.dart';
import '../../../core/widgets/language_selector.dart';

/// 系统配置（服务端模式）
/// 本轮改动：配置真正从服务端读取/保存（原先是空壳，点保存只弹提示）；
/// 新增「开机自启」与「启动服务端」（注册表 HKCU\\...\\Run）。
class ServerSettingsPage extends ConsumerStatefulWidget {
  const ServerSettingsPage({super.key});

  @override
  ConsumerState<ServerSettingsPage> createState() => _ServerSettingsPageState();
}

class _ServerSettingsPageState extends ConsumerState<ServerSettingsPage> {
  final _bindAddr = TextEditingController();
  final _port = TextEditingController();
  final _authPort = TextEditingController();
  final _webPort = TextEditingController();
  final _storageRoot = TextEditingController();
  final _maxUpload = TextEditingController();
  final _uploadSpeed = TextEditingController();
  final _downloadSpeed = TextEditingController();
  final _maxFileSize = TextEditingController();
  final _serverExe = TextEditingController();
  final _tunnelCommand = TextEditingController();
  final _serverName = TextEditingController();

  Map<String, dynamic> _remote = {};
  bool _loading = true;
  bool _saving = false;
  bool _autostart = false;
  String? _status;

  static const _runKey = r'HKCU\Software\Microsoft\Windows\CurrentVersion\Run';
  static const _runValue = 'FrostLeavesServer';

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    for (final c in [_bindAddr, _port, _authPort, _webPort, _storageRoot, _maxUpload,
      _uploadSpeed, _downloadSpeed, _maxFileSize, _serverExe, _tunnelCommand, _serverName]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _load() async {
    final api = ref.read(apiServiceProvider);
    String? err;
    try {
      final resp = await api.getSystemConfig();
      final cfg = resp['data'] as Map<String, dynamic>? ?? {};
      _remote = Map<String, dynamic>.from(cfg);
      _bindAddr.text = (cfg['http_bind_addr'] ?? '0.0.0.0').toString();
      _port.text = (cfg['http_port'] ?? 9090).toString();
      _authPort.text = (cfg['auth_port'] ?? 9091).toString();
      _webPort.text = (cfg['web_port'] ?? 9092).toString();
      _storageRoot.text = (cfg['storage_root'] ?? './storage').toString();
      _maxUpload.text = (cfg['max_upload_bytes'] ?? 0).toString();
      _uploadSpeed.text = (cfg['upload_speed_kbps'] ?? 0).toString();
      _downloadSpeed.text = (cfg['download_speed_kbps'] ?? 0).toString();
      _maxFileSize.text = (cfg['max_file_size_bytes'] ?? 0).toString();
      _tunnelCommand.text = (cfg['tunnel_command'] ?? 'mesh').toString();
      _serverName.text = (cfg['server_name'] ?? t(t('服务器'))).toString();
    } catch (e) {
      err = t(t('读取配置失败：$e'));
    }

    final prefs = await SharedPreferences.getInstance();
    _serverExe.text = prefs.getString('serverExePath') ?? '';
    final auto = await _isAutostartEnabled();

    if (!mounted) return;
    setState(() {
      _loading = false;
      _autostart = auto;
      _status = err;
    });
  }

  /// 需求 3：调用系统文件夹选择器挑选本机存储目录
  Future<void> _pickStorageDir() async {
    try {
      final dir = await FilePicker.platform.getDirectoryPath(
        dialogTitle: t(t('选择本机文件存储目录')),
      );
      if (dir == null || dir.isEmpty) return;
      setState(() => _storageRoot.text = dir);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(
            content: Text(t(t('已选择存储目录，点【保存配置】生效（重启服务端后完全生效）')))));
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text(t(t('选择目录失败：${ApiService.describeError(e)}')))));
      }
    }
  }

  Future<void> _save() async {
    setState(() { _saving = true; _status = null; });
    final api = ref.read(apiServiceProvider);
    final merged = Map<String, dynamic>.from(_remote);
    merged['http_bind_addr'] = _bindAddr.text.trim();
    merged['http_port'] = int.tryParse(_port.text.trim()) ?? 9090;
    merged['auth_port'] = int.tryParse(_authPort.text.trim()) ?? 9091;
    merged['web_port'] = int.tryParse(_webPort.text.trim()) ?? 9092;
    merged['storage_root'] = _storageRoot.text.trim();
    merged['max_upload_bytes'] = int.tryParse(_maxUpload.text.trim()) ?? 0;
    merged['upload_speed_kbps'] = int.tryParse(_uploadSpeed.text.trim()) ?? 0;
    merged['download_speed_kbps'] = int.tryParse(_downloadSpeed.text.trim()) ?? 0;
    merged['max_file_size_bytes'] = int.tryParse(_maxFileSize.text.trim()) ?? 0;
    merged['tunnel_command'] = _tunnelCommand.text.trim().isEmpty ? 'mesh' : _tunnelCommand.text.trim();
    merged['server_name'] = _serverName.text.trim().isEmpty ? t(t('服务器')) : _serverName.text.trim();

    final prefs = await SharedPreferences.getInstance();
    await prefs.setString('serverExePath', _serverExe.text.trim());

    try {
      await api.updateSystemConfig(merged);
      _remote = merged;
      setState(() => _status = t('已保存。端口/绑定地址的改动需要重启服务端才生效。'));
    } catch (e) {
      setState(() => _status = t(t('保存失败：$e')));
    }
    if (mounted) setState(() => _saving = false);
  }

  Future<bool> _isAutostartEnabled() async {
    if (!Platform.isWindows) return false;
    try {
      final r = await Process.run('reg', ['query', _runKey, '/v', _runValue]);
      return r.exitCode == 0;
    } catch (_) {
      return false;
    }
  }

  Future<void> _setAutostart(bool enable) async {
    if (!Platform.isWindows) {
      setState(() => _status = t(t('开机自启目前仅支持 Windows')));
      return;
    }
    final exe = _serverExe.text.trim();
    if (enable && exe.isEmpty) {
      setState(() => _status = t(t('请先填写服务端程序路径（例如 D:\\private-netdisk\\FrostLeaves_Server.exe）')));
      return;
    }
    try {
      ProcessResult r;
      if (enable) {
        r = await Process.run('reg', ['add', _runKey, '/v', _runValue, '/t', 'REG_SZ', '/d', exe, '/f']);
      } else {
        r = await Process.run('reg', ['delete', _runKey, '/v', _runValue, '/f']);
      }
      final ok = r.exitCode == 0;
      final now = await _isAutostartEnabled();
      setState(() {
        _autostart = now;
        _status = ok ? (now ? t(t('已开启开机自启')) : t(t('已关闭开机自启'))) : t(t('操作失败：${r.stderr}'));
      });
    } catch (e) {
      setState(() => _status = t(t('操作失败：$e')));
    }
  }

  Future<void> _startServer() async {
    final exe = _serverExe.text.trim();
    if (exe.isEmpty) {
      setState(() => _status = t(t('请先填写服务端程序路径')));
      return;
    }
    if (!File(exe).existsSync()) {
      setState(() => _status = t(t('找不到文件：$exe')));
      return;
    }
    try {
      await Process.start(exe, const [], mode: ProcessStartMode.detached);
      setState(() => _status = t(t('已尝试启动服务端（请稍候刷新仪表盘确认）')));
    } catch (e) {
      setState(() => _status = t(t('启动失败：$e')));
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    return SingleChildScrollView(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _section(t(t('外观')), [const ThemeModeSelector(), const SizedBox(height: 14), const LanguageSelector()]),
          const SizedBox(height: 16),
          Text(t(t('系统配置')),
              style: TextStyle(fontSize: 24, fontWeight: FontWeight.bold, color: AppTheme.textPrimary)),
          const SizedBox(height: 16),
          _section(t(t('网络配置')), [
            _field(t(t('HTTP网关绑定地址')), _bindAddr, hint: t(t('0.0.0.0 允许局域网访问'))),
            _field(t(t('文件网关端口')), _port),
            _field(t(t('授权服务端口')), _authPort),
            _field(t(t('Web 服务端口')), _webPort),
          ]),
          _section(t(t('存储配置')), [
            _field(t(t('本机文件存储地址')), _storageRoot),
      Align(
        alignment: Alignment.centerRight,
        child: ElevatedButton.icon(
          onPressed: _pickStorageDir,
          icon: const Icon(Icons.folder_open, size: 16),
          label: Text(t(t('修改'))),
        ),
      ),
          ]),
          _section(t(t('全局默认配额（0=不限制）')), [
            _field(t(t('单客户端总上传上限 (bytes)')), _maxUpload),
            _field(t(t('上传限速 (KB/s)')), _uploadSpeed),
            _field(t(t('下载限速 (KB/s)')), _downloadSpeed),
            _field(t(t('单文件最大尺寸 (bytes)')), _maxFileSize),
          ]),
          _section(t(t('服务器信息（需求 2）')), [
            _field(t(t('服务器名称（客户端与标题栏显示）')), _serverName, hint: t(t('默认「服务器」，例如：蒜叶的服务器'))),
          ]),
          _section(t(t('常驻与公网（本轮新增）')), [
            _field(t(t('服务端程序路径')), _serverExe, hint: t(r'例如 D:\private-netdisk\FrostLeaves_Server.exe')),
            _field(t(t('组网命令（进阶，默认自动探测）')), _tunnelCommand),
            Row(
              children: [
                SizedBox(
                  width: 220,
                  child: Row(
                    children: [
                      Switch(value: _autostart, onChanged: _setAutostart),
                      Text(t(t('开机自启服务端')), style: TextStyle(fontSize: 13, color: AppTheme.textSecondary)),
                    ],
                  ),
                ),
                const SizedBox(width: 12),
                OutlinedButton.icon(
                  onPressed: _startServer,
                  icon: const Icon(Icons.play_arrow, size: 18),
                  label: Text(t(t('立即启动服务端'))),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              t(t('说明：桌面端只负责管理，服务端是独立的 FrostLeaves_Server.exe；关掉本窗口服务仍然运行。')),
              style: TextStyle(fontSize: 12, color: AppTheme.textSecondary),
            ),
          ]),
          const SizedBox(height: 8),
          Row(
            children: [
              ElevatedButton(
                onPressed: _saving ? null : _save,
                child: Text(_saving ? t(t('保存中…')) : t(t('保存配置'))),
              ),
              const SizedBox(width: 12),
              OutlinedButton(onPressed: _load, child: Text(t(t('重新读取')))),
            ],
          ),
          if (_status != null) ...[
            const SizedBox(height: 12),
            Text(_status!, style: TextStyle(fontSize: 13, color: AppTheme.warningColor)),
          ],
        ],
      ),
    );
  }

  Widget _section(String title, List<Widget> children) {
    return Container(
      width: double.infinity,
      margin: const EdgeInsets.only(bottom: 16),
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: AppTheme.cardColor,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: AppTheme.borderColor),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(title, style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: AppTheme.textPrimary)),
          const SizedBox(height: 16),
          ...children,
        ],
      ),
    );
  }

  Widget _field(String label, TextEditingController controller, {String? hint}) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label, style: TextStyle(fontSize: 13, color: AppTheme.textSecondary)),
          const SizedBox(height: 6),
          TextField(controller: controller, decoration: InputDecoration(hintText: hint, isDense: true)),
        ],
      ),
    );
  }
}
