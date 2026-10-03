import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';
import '../../../core/services/firewall_service.dart';

/// 分享管理（服务端模式）：生成访客链接、查看/吊销分享、开关公网访问
class ShareManagementPage extends ConsumerStatefulWidget {
  const ShareManagementPage({super.key});

  @override
  ConsumerState<ShareManagementPage> createState() => _ShareManagementPageState();
}

class _ShareManagementPageState extends ConsumerState<ShareManagementPage> {
  List<Map<String, dynamic>> _shares = [];
  Map<String, dynamic>? _tunnel;
  String? _fwSummary;  // 需求 6：防火墙放行结果摘要
  Map<String, dynamic>? _netInfo; // LAN 地址 / 公网地址（用于生成分享链接与二维码）
  bool _loading = true;
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  ApiService get _api => ref.read(apiServiceProvider);

  /// 访客页端口 = 认证端口 + 1（9091 -> 9092），与后端约定一致
  String get _webBase {
    try {
      final u = Uri.parse(_api.authUrl);
      return u.port > 1 ? '${u.scheme}://${u.host}:${u.port + 1}' : '${u.scheme}://${u.host}';
    } catch (_) {
      return '';
    }
  }

  /// LAN 基础地址：优先用服务端探测到的局域网 IP（避免给出 127.0.0.1 导致手机打不开）
  String get _lanBase {
    final ips = (_netInfo?['lan_ips'] as List?) ?? const [];
    final port = _netInfo?['web_port'] ?? 9092;
    if (ips.isNotEmpty) {
      // 局域网已启用 HTTPS（自签 CA；首次需安装证书或扫描引导端口）
      return 'https://${ips.first}:$port';
    }
    return _webBase;
  }

  /// 公网基础地址（Funnel 已开启时非空）
  String get _publicBase => (_netInfo?['tunnel_url'] ?? '').toString();

  String _lanLink(String code) => '$_lanBase/s/$code';
  String _publicLink(String code) {
    final base = _publicBase;
    return base.isEmpty ? '' : '$base/s/$code';
  }

  String _link(String code) => _lanLink(code);

  /// 二维码弹窗（服务端生成 PNG，手机相机可直接扫）
  Future<void> _showQr(String url) async {
    if (!mounted) return;
    // 二维码由服务端生成；管理接口需要管理令牌，且服务端模式下需走回环，
    // 所以统一用 ApiService 拉字节（Image.network 不带请求头，会被 403）。
    await showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t('扫码打开')),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            SizedBox(
              width: 260,
              height: 260,
              child: FutureBuilder<List<int>>(
                future: _api.fetchQrPng(url),
                builder: (c, snap) {
                  if (snap.connectionState != ConnectionState.done) {
                    return const Center(
                      child: SizedBox(
                          width: 28, height: 28, child: CircularProgressIndicator(strokeWidth: 2)),
                    );
                  }
                  final bytes = snap.data;
                  if (snap.hasError || bytes == null || bytes.isEmpty) {
                    return Center(
                      child: Text(t('二维码加载失败（请确认服务端已更新）'),
                          textAlign: TextAlign.center),
                    );
                  }
                  return Image.memory(Uint8List.fromList(bytes),
                      width: 260, height: 260, gaplessPlayback: true);
                },
              ),
            ),
            const SizedBox(height: 12),
            SelectableText(url, style: const TextStyle(fontSize: 12)),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () {
              Clipboard.setData(ClipboardData(text: url));
              Navigator.pop(ctx);
            },
            child: Text(t('复制链接')),
          ),
          TextButton(onPressed: () => Navigator.pop(ctx), child: Text(t('关闭'))),
        ],
      ),
    );
  }

  Future<void> _refresh() async {
    setState(() { _loading = true; _error = null; });
    try {
      final resp = await _api.listShares();
      final data = resp['data'] as Map<String, dynamic>?;
      final items = (data?['items'] as List?) ?? [];
      final shares = items
          .map((e) => Map<String, dynamic>.from(e as Map))
          .toList()
        ..sort((a, b) =>
            (b['created_at'] ?? '').toString().compareTo((a['created_at'] ?? '').toString()));

      Map<String, dynamic>? netInfo;
      try {
        final nr = await _api.getSystemNetwork();
        netInfo = nr['data'] as Map<String, dynamic>?;
      } catch (_) {}

      Map<String, dynamic>? tunnel;
      try {
        final st = await _api.getTunnelStatus();
        tunnel = st['data'] as Map<String, dynamic>?;
      } catch (_) {}

      if (!mounted) return;
      setState(() { _shares = shares; _tunnel = tunnel; _netInfo = netInfo; _loading = false; });
    } catch (e) {
      if (!mounted) return;
      setState(() { _loading = false; _error = t('加载失败：$e'); });
    }
  }

  void _toast(String msg, {bool error = false}) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(
      content: Text(msg),
      backgroundColor: error ? AppTheme.errorColor : null,
    ));
  }

  void _copy(String text) {
    Clipboard.setData(ClipboardData(text: text));
    _toast(t('链接已复制：$text'));
  }

  Future<void> _openInBrowser(String url) async {
    if (!Platform.isWindows) { _copy(url); return; }
    await Process.run('cmd', ['/c', 'start', '', url]);
  }

  Future<void> _revoke(String code) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t('吊销这个分享？')),
        content: Text(t('该分享链接将立即失效，已发出去的链接都不能再用。')),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: Text(t('取消'))),
          ElevatedButton(onPressed: () => Navigator.pop(ctx, true), child: Text(t('吊销'))),
        ],
      ),
    );
    if (ok != true) return;
    try {
      await _api.revokeShare(code);
      _toast(t('已吊销'));
      _refresh();
    } catch (e) {
      _toast(t('吊销失败：$e'), error: true);
    }
  }

  Future<void> _tunnelAction(bool enable) async {
    setState(() => _busy = true);
    try {
      if (enable) {
        final resp = await _api.enableTunnel();
        final url = (resp['data'] as Map<String, dynamic>?)?['url']?.toString() ?? '';
        _toast(t('公网访问已开启${url.isEmpty ? "" : "：$url"}'));
      } else {
        await _api.disableTunnel();
        _toast(t('公网访问已关闭'));
      }
    } catch (e) {
      await _showDetail(t('开启/关闭公网访问失败'), _fmtError(e));
    }
    setState(() => _busy = false);
    _refresh();
  }

  Future<void> _showCreateDialog() async {
    final targetCtrl = TextEditingController(text: 'shared/');
    final noteCtrl = TextEditingController();
    final hoursCtrl = TextEditingController(text: '24');
    final maxDlCtrl = TextEditingController(text: '0');
    String perm = 'read';

    final created = await showDialog<Map<String, dynamic>>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setLocal) => AlertDialog(
          title: Text(t('新建分享')),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(t('目录（相对 storage，例如 shared/照片）'),
                    style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
                TextField(controller: targetCtrl),
                SizedBox(height: 12),
                Text(t('权限'), style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
                DropdownButton<String>(
                  value: perm,
                  isExpanded: true,
                  items: [
                    DropdownMenuItem(value: 'read', child: Text(t('只读（可看/可下载）'))),
                    DropdownMenuItem(value: 'upload', child: Text(t('只读 + 可上传（投递箱）'))),
                    DropdownMenuItem(value: 'write', child: Text(t('完整读写（慎用）'))),
                  ],
                  onChanged: (v) => setLocal(() => perm = v ?? 'read'),
                ),
                const SizedBox(height: 12),
                Text(t('有效期（小时，0=默认24h，-1=永久）'),
                    style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
                TextField(controller: hoursCtrl, keyboardType: TextInputType.number),
                const SizedBox(height: 12),
                Text(t('最多下载次数（0=不限）'),
                    style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
                TextField(controller: maxDlCtrl, keyboardType: TextInputType.number),
                const SizedBox(height: 12),
                Text(t('备注（访客打开时看到的标题）'),
                    style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
                TextField(controller: noteCtrl),
              ],
            ),
          ),
          actions: [
            TextButton(onPressed: () => Navigator.pop(ctx), child: Text(t('取消'))),
            ElevatedButton(
              onPressed: () {
                final target = targetCtrl.text.trim();
                if (target.isEmpty) return;
                Navigator.pop(ctx, {
                  'target': target,
                  'perm': perm,
                  'expires_hours': int.tryParse(hoursCtrl.text.trim()) ?? 24,
                  'max_downloads': int.tryParse(maxDlCtrl.text.trim()) ?? 0,
                  'note': noteCtrl.text.trim(),
                });
              },
              child: Text(t('创建')),
            ),
          ],
        ),
      ),
    );

    if (created == null) return;
    setState(() => _busy = true);
    try {
      final resp = await _api.createShare(
        target: created['target'] as String,
        perm: created['perm'] as String,
        expiresHours: created['expires_hours'] as int,
        maxDownloads: created['max_downloads'] as int,
        note: created['note'] as String,
      );
      final data = resp['data'] as Map<String, dynamic>?;
      final code = data?['code']?.toString() ?? '';
      if (code.isNotEmpty) {
        _showCopyOptions(code);
      }
      _refresh();
    } catch (e) {
      _toast(t('创建失败：$e'), error: true);
    }
    setState(() => _busy = false);
  }

  /// 需求 7：把异常里的原始输出格式化出来（dio 的 response.data 里有 error/output）
  String _fmtError(Object e) {
    try {
      final dynamic de = e;
      final dynamic data = de.response?.data;
      if (data is Map) {
        final err = (data['error'] ?? '').toString();
        final out = (data['output'] ?? '').toString();
        if (out.isNotEmpty) {
          return t('$err\n\n--- 原始输出 ---\n$out');
        }
        if (err.isNotEmpty) return err;
      }
    } catch (_) {}
    return ApiService.describeError(e);
  }

  /// 需求 7：显示详细信息（可选中复制）
  Future<void> _showDetail(String title, String body) async {
    if (!mounted) return;
    await showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(title),
        content: SizedBox(
          width: 560,
          child: SingleChildScrollView(
            child: SelectableText(
              body.isEmpty ? t('（无更多信息）') : body,
              style: const TextStyle(fontSize: 12),
            ),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () {
              Clipboard.setData(ClipboardData(text: body));
              Navigator.pop(ctx);
            },
            child: Text(t('复制')),
          ),
          TextButton(onPressed: () => Navigator.pop(ctx), child: Text(t('关闭'))),
        ],
      ),
    );
  }

  /// 需求 7：探测本机 组网 CLI 是否可用
  Future<void> _detectTunnel() async {
    setState(() => _busy = true);
    try {
      final resp = await _api.detectTunnel();
      final data = resp['data'] as Map<String, dynamic>?;
      final available = data?['available'] == true;
      final exe = (data?['exe'] ?? '').toString();
      final version = (data?['version'] ?? '').toString();
      final message = (data?['message'] ?? '').toString();
      await _showDetail(
        available ? t('组网客户端可用') : t('未检测到可用的组网客户端'),
        t('状态：${available ? "可用" : "不可用"}\n路径：${exe.isEmpty ? "-" : exe}\n版本：${version.isEmpty ? "-" : version}\n说明：$message'),
      );
    } catch (e) {
      await _showDetail(t('探测失败'), _fmtError(e));
    }
    if (mounted) setState(() => _busy = false);
  }

  /// 需求 6：读取待放行端口（取服务端配置，失败则用默认值）
  Future<List<int>> _resolvePorts() async {
    try {
      final resp = await _api.getSystemConfig();
      final cfg = resp['data'] as Map<String, dynamic>?;
      final ports = <int>[];
      for (final k in ['http_port', 'auth_port', 'web_port']) {
        final v = cfg?[k];
        if (v is int && v > 0) ports.add(v);
      }
      if (ports.isNotEmpty) return ports;
    } catch (_) {}
    return const [9090, 9091, 9092, 9093];
  }

  /// 需求 6 方案 1：自动放行防火墙端口（会弹 UAC）
  Future<void> _openFirewall() async {
    setState(() => _busy = true);
    try {
      final ports = await _resolvePorts();
      final results = await FirewallService.ensurePorts(ports);
      final summary = results.map((r) => t('${r.port}: ${r.ok ? t("已放行") : t("失败")}（${r.message}）')).join('\n');
      final allOk = results.every((r) => r.ok);
      setState(() => _fwSummary = summary);
      if (allOk) {
        _toast(t('已放行：${ports.join(", ")}'));
      } else {
        await _showDetail(t('部分端口未能放行'), '$summary\n\n${FirewallService.manualGuide(ports)}');
      }
    } catch (e) {
      final ports = await _resolvePorts();
      await _showDetail(t('放行失败'), '$e\n\n${FirewallService.manualGuide(ports)}');
    }
    if (mounted) setState(() => _busy = false);
  }

  /// HTTPS：把 CA 安装到当前用户的受信任根证书（浏览器/系统不再告警）
  Future<void> _installCa() async {
    setState(() => _busy = true);
    try {
      final bytes = await _api.fetchCaPemBytes();
      final tmp = File('${Directory.systemTemp.path}/frostleaves-ca.crt');
      await tmp.writeAsBytes(bytes);
      final r = await Process.run('certutil', ['-addstore', '-user', 'Root', tmp.path]);
      final ok = r.exitCode == 0;
      await _showDetail(
        ok ? t('证书已安装（当前用户）') : t('证书安装失败'),
        t('命令：certutil -addstore -user Root <CA>\n退出码：${r.exitCode}\n\n${r.stdout}\n${r.stderr}'),
      );
    } catch (e) {
      await _showDetail(t('证书安装失败'), '$e');
    }
    if (mounted) setState(() => _busy = false);
  }

  /// HTTPS：查看证书指纹（用于手机端核对）
  Future<void> _showTlsInfo() async {
    try {
      final resp = await _api.getTlsInfo();
      final d = resp['data'] as Map<String, dynamic>?;
      final sans = (d?['sans'] as List?)?.join(', ') ?? '';
      await _showDetail(
        t('证书信息'),
        t('指纹(SHA-256)：\n${d?["fingerprint"] ?? "-"}\n\n覆盖地址：\n$sans\n\nCA 下载：\n${d?["ca_url"] ?? "-"}\n\nHTTPS 网页：\n${d?["https_web"] ?? "-"}'),
      );
    } catch (e) {
      await _showDetail(t('读取证书信息失败'), '$e');
    }
  }

  /// 需求 2：复制选项（内网网址 / 公网网址）
  Future<void> _showCopyOptions(String code) async {
    final lan = _lanLink(code);
    final pub = _publicLink(code);
    await showDialog<void>(
      context: context,
      builder: (ctx) => SimpleDialog(
        title: Text(t('复制分享')),
        children: [
          SimpleDialogOption(
            onPressed: () {
              Clipboard.setData(ClipboardData(text: lan));
              Navigator.pop(ctx);
              _toast(t('已复制内网网址'));
            },
            child: ListTile(
              leading: const Icon(Icons.lan),
              title: Text(t('复制内网网址')),
              subtitle: Text(lan, maxLines: 1, overflow: TextOverflow.ellipsis),
            ),
          ),
          if (pub.isNotEmpty)
            SimpleDialogOption(
              onPressed: () {
                Clipboard.setData(ClipboardData(text: pub));
                Navigator.pop(ctx);
                _toast(t('已复制公网网址'));
              },
              child: ListTile(
                leading: const Icon(Icons.public),
                title: Text(t('复制公网网址')),
                subtitle: Text(pub, maxLines: 1, overflow: TextOverflow.ellipsis),
              ),
            ),
        ],
      ),
    );
  }

  /// 需求 6 方案 2：手动配置引导
  Future<void> _showManualFirewallGuide() async {
    final ports = await _resolvePorts();
    await _showDetail(t('手动放行防火墙（完整步骤）'), FirewallService.manualGuide(ports));
  }

  String _shareStatus(Map<String, dynamic> s) {
    if (s['revoked'] == true) return t('已吊销');
    final exp = s['expires_at']?.toString() ?? '';
    if (exp.isNotEmpty && !exp.startsWith('0001-')) {
      final dt = DateTime.tryParse(exp);
      if (dt != null && dt.isBefore(DateTime.now())) return t('已过期');
    }
    final maxDl = (s['max_downloads'] ?? 0) as int;
    final dl = (s['downloads'] ?? 0) as int;
    if (maxDl > 0 && dl >= maxDl) return t('已达下载上限');
    return t('有效');
  }

  String _expText(Map<String, dynamic> s) {
    final exp = s['expires_at']?.toString() ?? '';
    if (exp.isEmpty || exp.startsWith('0001-')) return t('永久');
    final dt = DateTime.tryParse(exp);
    if (dt == null) return exp;
    return t('至 ${dt.toLocal().toString().substring(0, 16)}');
  }

  @override
  Widget build(BuildContext context) {
    final tunnelEnabled = _tunnel?['enabled'] == true;
    final tunnelUrl = _tunnel?['url']?.toString() ?? '';

    return SingleChildScrollView(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(t('分享管理'),
                  style: TextStyle(fontSize: 24, fontWeight: FontWeight.bold, color: AppTheme.textPrimary)),
              const Spacer(),
              IconButton(onPressed: _refresh, icon: const Icon(Icons.refresh), tooltip: t('刷新')),
              const SizedBox(width: 8),
              ElevatedButton.icon(
                onPressed: _busy ? null : _showCreateDialog,
                icon: const Icon(Icons.add_link, size: 18),
                label: Text(t('新建分享')),
              ),
            ],
          ),
          const SizedBox(height: 16),
          _card(
            title: t('公网访问（点对点私有组网）'),
            children: [
              Text(
                tunnelEnabled
                    ? t('已开启：$tunnelUrl')
                    : t('未开启（未开启时只能在局域网内访问；开启后亲友可通过公网 HTTPS 地址访问）'),
                style: TextStyle(fontSize: 13, color: AppTheme.textSecondary),
              ),
              const SizedBox(height: 12),
              Row(
                children: [
                  ElevatedButton(
                    onPressed: _busy ? null : () => _tunnelAction(!tunnelEnabled),
                    child: Text(tunnelEnabled ? t('关闭公网访问') : t('开启公网访问')),
                  ),
                  const SizedBox(width: 12),
                  if (tunnelEnabled && tunnelUrl.isNotEmpty)
                    OutlinedButton(
                      onPressed: () => _openInBrowser(tunnelUrl),
                      child: Text(t('在浏览器打开')),
                    ),
                  const SizedBox(width: 12),
                  OutlinedButton(
                    onPressed: _busy ? null : _detectTunnel,
                    child: Text(t('检测环境')),
                    ),
                ],
              ),
              const SizedBox(height: 8),
              Text(
                t('前置条件：本机已安装 点对点私有组网客户端并完成登录；首次使用需在 组网客户端后台为组网启用公网访问。'),
                style: TextStyle(fontSize: 12, color: AppTheme.textSecondary.withOpacity(0.8)),
              ),
            ],
          ),
          const SizedBox(height: 8),
          _card(
            title: t('HTTPS 证书（局域网加密）'),
            children: [
              Text(
                t('局域网已启用 HTTPS（自签证书）。在本机装一次证书后，浏览器访问 https://<局域网IP>:9092 就不会再告警。'),
                style: TextStyle(fontSize: 13, color: AppTheme.textSecondary),
              ),
              const SizedBox(height: 12),
              Row(
                children: [
                  ElevatedButton(
                    onPressed: _busy ? null : _installCa,
                    child: Text(t('安装证书到本机')),
                  ),
                  const SizedBox(width: 12),
                  OutlinedButton(
                    onPressed: _showTlsInfo,
                    child: Text(t('查看指纹')),
                  ),
                ],
              ),
            ],
          ),
          _card(
            title: t('局域网访问（Windows 防火墙，需求 6）'),
            children: [
              Text(
                t('手机/浏览器连不上，通常是 Windows 防火墙拦了这几个端口。点下面按钮自动放行（会弹 UAC 授权窗口）；若失败可查看手动步骤。'),
                style: TextStyle(fontSize: 13, color: AppTheme.textSecondary),
              ),
              const SizedBox(height: 12),
              Row(
                children: [
                  ElevatedButton(
                    onPressed: _busy ? null : _openFirewall,
                    child: Text(t('放行防火墙端口')),
                  ),
                  const SizedBox(width: 12),
                  OutlinedButton(
                    onPressed: _showManualFirewallGuide,
                    child: Text(t('查看手动步骤')),
                  ),
                ],
              ),
              if (_fwSummary != null) ...[
                const SizedBox(height: 10),
                Text(_fwSummary!, style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
              ],
            ],
          ),
          if (_loading) const Padding(padding: EdgeInsets.all(24), child: Center(child: CircularProgressIndicator())),
          if (_error != null)
            Padding(padding: const EdgeInsets.all(12), child: Text(_error!, style: TextStyle(color: AppTheme.errorColor))),
          if (!_loading && _shares.isEmpty && _error == null)
            _card(title: t('还没有分享'), children: [
              Text(t('点右上角「新建分享」，选一个目录，就能生成给亲友的链接。'),
                  style: TextStyle(fontSize: 13, color: AppTheme.textSecondary)),
            ]),
          for (final s in _shares) _shareTile(s),
        ],
      ),
    );
  }

  Widget _card({required String title, required List<Widget> children}) {
    return Container(
      width: double.infinity,
      margin: const EdgeInsets.only(bottom: 12),
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
          const SizedBox(height: 12),
          ...children,
        ],
      ),
    );
  }

  Widget _shareTile(Map<String, dynamic> s) {
    final code = s['code']?.toString() ?? '';
    final status = _shareStatus(s);
    final active = status == t('有效');
    final perm = s['perm']?.toString() ?? 'read';
    final permText = perm == 'read' ? t('只读') : (perm == 'upload' ? t('可上传') : t('读写'));
    final link = _link(code);

    return Container(
      margin: const EdgeInsets.only(bottom: 12),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: AppTheme.cardColor,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: AppTheme.borderColor),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(code, style: TextStyle(fontSize: 15, fontWeight: FontWeight.w700, color: AppTheme.textPrimary)),
              const SizedBox(width: 10),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                decoration: BoxDecoration(
                  color: active ? AppTheme.successColor.withOpacity(0.15) : AppTheme.borderColor,
                  borderRadius: BorderRadius.circular(999),
                ),
                child: Text(status,
                    style: TextStyle(fontSize: 11, color: active ? AppTheme.successColor : AppTheme.textSecondary)),
              ),
              const Spacer(),
              Text(t('$permText · ${_expText(s)} · 下载 ${s["downloads"] ?? 0}'),
                  style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
            ],
          ),
          if ((s['note']?.toString() ?? '').isNotEmpty) ...[
            const SizedBox(height: 6),
            Text(s['note'].toString(), style: TextStyle(fontSize: 13, color: AppTheme.textPrimary)),
          ],
          const SizedBox(height: 6),
          Text(t('目录：${s["target"] ?? ""}'), style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
          const SizedBox(height: 4),
          SelectableText(_lanLink(code),
              style: TextStyle(fontSize: 12, color: AppTheme.primaryColor)),
          if (_publicLink(code).isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: SelectableText(t('公网：${_publicLink(code)}'),
                  style: TextStyle(fontSize: 12, color: AppTheme.successColor)),
            ),
          const SizedBox(height: 10),
          Row(
            children: [
              OutlinedButton.icon(
                onPressed: () => _showCopyOptions(code),
                icon: const Icon(Icons.copy, size: 16),
                label: Text(t('复制链接')),
              ),
              const SizedBox(width: 8),
              OutlinedButton.icon(
                onPressed: () => _openInBrowser(link),
                icon: const Icon(Icons.open_in_browser, size: 16),
                label: Text(t('预览')),
              ),
              const SizedBox(width: 8),
              OutlinedButton.icon(
                onPressed: () => _showQr(_lanLink(code)),
                icon: const Icon(Icons.qr_code, size: 16),
                label: Text(t('二维码')),
              ),
              const SizedBox(width: 8),
              if (active)
                TextButton.icon(
                  onPressed: () => _revoke(code),
                  icon: Icon(Icons.block, size: 16, color: AppTheme.errorColor),
                  label: Text(t('吊销'), style: TextStyle(color: AppTheme.errorColor)),
                ),
            ],
          ),
        ],
      ),
    );
  }
}
