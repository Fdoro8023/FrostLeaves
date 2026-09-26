import 'dart:async';

import 'dart:io';

import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';

/// 关于 Frost Leaves：CPU / 内存 / 磁盘占用可视化 + 版本信息
class AboutPage extends ConsumerStatefulWidget {
  const AboutPage({super.key});
  @override
  ConsumerState<AboutPage> createState() => _AboutPageState();
}
class _AboutPageState extends ConsumerState<AboutPage> {
  Map<String, dynamic>? _m;
  Map<String, dynamic>? _tls;
  bool _loading = true;
  String? _error;
  Timer? _timer; // 需求：每秒刷新一次
  String _envText = '-'; // 当前系统环境（如 Windows 11 Pro (Build 26200) · x64）

  @override
  void initState() {
    super.initState();
    _load();
    // 需求：CPU / 内存 / 文件存储 每秒刷新
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) _load(silent: true);
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  Future<void> _load({bool silent = false}) async {
    if (!silent) setState(() { _loading = true; _error = null; });
    try {
      final api = ref.read(apiServiceProvider);
      final resp = await api.getSystemMetrics();
      Map<String, dynamic>? tls;
      try {
        final tlsResp = await api.getTlsInfo();
        tls = tlsResp['data'] as Map<String, dynamic>?;
      } catch (_) {}
      try {
        _envText = _withArch(await api.resolveDeviceModel());
      } catch (_) {}
      if (!mounted) return;
      setState(() {
        _m = resp['data'] as Map<String, dynamic>?;
        _tls = tls;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() { _loading = false; _error = ApiService.describeError(e); });
    }
  }

  /// 系统环境文案：Windows 追加架构标识（x64 / ARM64）
  String _withArch(String base) {
    if (Platform.isWindows) {
      final arch =
          (Platform.environment['PROCESSOR_ARCHITECTURE'] ?? '').toUpperCase();
      final archTxt = arch.contains('ARM64') ? 'ARM64' : 'x64';
      return '$base · $archTxt';
    }
    return base;
  }

  static String _fmtBytes(num? v) {
    if (v == null) return '-';
    double b = v.toDouble();
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    int i = 0;
    while (b >= 1024 && i < units.length - 1) {
      b /= 1024;
      i++;
    }
    return '${b.toStringAsFixed(i == 0 ? 0 : 1)} ${units[i]}';
  }

  @override
  Widget build(BuildContext context) {
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    final m = _m;
    return SingleChildScrollView(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(t('关于 Frost Leaves'),
                  style: TextStyle(fontSize: 24, fontWeight: FontWeight.bold, color: AppTheme.textPrimary)),
              const Spacer(),
              IconButton(onPressed: _load, icon: const Icon(Icons.refresh), tooltip: t('刷新')),
            ],
          ),
          const SizedBox(height: 8),
          Text(t('绿色 = Frost Leaves 占用 · 蓝色 = 其他程序 · 灰色 = 空闲'),
              style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
          const SizedBox(height: 20),
          if (_error != null)
            Container(
              padding: const EdgeInsets.all(12),
              margin: const EdgeInsets.only(bottom: 16),
              decoration: BoxDecoration(
                color: AppTheme.errorColor.withOpacity(0.1),
                borderRadius: BorderRadius.circular(8),
              ),
              child: Text(t('读取系统信息失败：$_error'),
                  style: TextStyle(fontSize: 12, color: AppTheme.errorColor)),
            ),
          if (m != null) ...[
            _usageBar(
              title: 'CPU',
              total: 100,
              segments: [
                _seg('Frost Leaves', (m['cpu_app'] ?? 0).toDouble(), AppTheme.successColor),
                _seg(t('其他程序'), ((m['cpu_percent'] ?? 0).toDouble() - (m['cpu_app'] ?? 0).toDouble()).clamp(0, 100).toDouble(), const Color(0xFF3B82F6)),
                _seg(t('空闲'), (100 - (m['cpu_percent'] ?? 0).toDouble()).clamp(0, 100).toDouble(), const Color(0xFF6B7280)),
              ],
              totalText: t('100% · ${m["cpu_cores"] ?? "-"} 核'),
            ),
            _usageBar(
              title: t('内存'),
              total: (m['mem_total'] ?? 0).toDouble(),
              segments: [
                _seg('Frost Leaves', (m['mem_app'] ?? 0).toDouble(), AppTheme.successColor),
                _seg(t('其他程序'), ((m['mem_used'] ?? 0).toDouble() - (m['mem_app'] ?? 0).toDouble()).clamp(0, 1e18).toDouble(), const Color(0xFF3B82F6)),
                _seg(t('空闲'), ((m['mem_total'] ?? 0).toDouble() - (m['mem_used'] ?? 0).toDouble()).clamp(0, 1e18).toDouble(), const Color(0xFF6B7280)),
              ],
              totalText: _fmtBytes(m['mem_total'] as num?),
            ),
            _usageBar(
              title: t('文件存储'),
              total: (m['disk_total'] ?? 0).toDouble(),
              segments: [
                _seg('Frost Leaves', (m['disk_app'] ?? 0).toDouble(), AppTheme.successColor),
                _seg(t('其他程序'), ((m['disk_used'] ?? 0).toDouble() - (m['disk_app'] ?? 0).toDouble()).clamp(0, 1e18).toDouble(), const Color(0xFF3B82F6)),
                _seg(t('空闲'), (m['disk_free'] ?? 0).toDouble(), const Color(0xFF6B7280)),
              ],
              totalText: _fmtBytes(m['disk_total'] as num?),
            ),
          ],
          const SizedBox(height: 24),
          _infoCard(
            title: t('版本信息'),
            rows: [
              [t('版本号'), 'v${m?['version'] ?? '1.0.0 beta'}'],
              [t('服务器名称'), (_tls?['server_name'] ?? t('服务器')).toString()],
              [t('存储目录'), (m?['disk_path'] ?? '-').toString()],
              [t('Frost Leaves 存储占用'), _fmtBytes(m?['disk_app'] as num?)],
              [t('服务端口'), '9090/9091/9092/9093'],
              [t('HTTPS 证书指纹'), (_tls?['fingerprint'] ?? t('未启用')).toString()],
            ],
          ),
          const SizedBox(height: 16),
          _infoCard(
            title: t('说明'),
            rows: [
              [t('数据来源'), t('本地服务端实时采样（每秒刷新）')],
              [t('CPU 占用'), t('本进程占全部逻辑核心的百分比')],
              [t('绿色份额'), t('仅统计 Frost Leaves 自身进程')],
              [t('磁盘统计'), t('所在卷的总量 / 已用 / 空闲')],
              [t('用户协议'), t('完整 FrostLeaves 用户许可协议存放于程序目录 user_agreement.txt')],
            ],
          ),
          const SizedBox(height: 16),
          Container(
            padding: const EdgeInsets.all(20),
            decoration: BoxDecoration(
              color: AppTheme.cardColor,
              borderRadius: BorderRadius.circular(12),
              border: Border.all(color: AppTheme.borderColor),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(t('系统环境与兼容性要求'),
                    style: TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w600,
                        color: AppTheme.textPrimary)),
                const SizedBox(height: 12),
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    SizedBox(
                        width: 160,
                        child: Text(t('当前系统'),
                            style: TextStyle(
                                fontSize: 13, color: AppTheme.textSecondary))),
                    Expanded(
                        child: SelectableText(_envText,
                            style: TextStyle(
                                fontSize: 13, color: AppTheme.textPrimary))),
                  ],
                ),
                const SizedBox(height: 18),
                Text(
                  t('本软件Windows服务端仅支持x64架构，兼容 Windows 10 1809 及以上系统；Android客户端最低兼容 Android 5.0（推荐 Android 8.0 及以上版本）'),
                  style: TextStyle(fontSize: 12, height: 1.5, color: AppTheme.textSecondary),
                ),
              ],
            ),
          ),
          const SizedBox(height: 24),
          Container(
            width: double.infinity,
            padding: const EdgeInsets.all(20),
            decoration: BoxDecoration(
              color: AppTheme.cardColor,
              borderRadius: BorderRadius.circular(12),
              border: Border.all(color: AppTheme.borderColor),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(t('开发者：霜叶  2726895865@qq.com'),
                    style: TextStyle(fontSize: 13, color: AppTheme.textPrimary)),
                const SizedBox(height: 10),
                InkWell(
                  onTap: _showFeedback,
                  child: Padding(
                    padding: EdgeInsets.symmetric(vertical: 6),
                    child: Text(t('遇到 bug？联系我们'),
                        style: TextStyle(
                          fontSize: 14,
                          color: AppTheme.primaryColor,
                          decoration: TextDecoration.underline,
                        )),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  /// 需求 4：反馈弹窗（复制邮箱 / 关闭）
  Future<void> _showFeedback() async {
    const email = '2726895865@qq.com';
    await showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('关于反馈'),
        content: SingleChildScrollView(
          child: Text(
            t('由于开发者团队属于高中生，Bug 修复与版本迭代会很慢，敬请谅解。\n\n'
              '但我们接受您的反馈，请您通过发邮件的方式向我们说明问题，并附带上问题的截图、日志、报错信息等，感谢您对 Frost Leaves 的支持与信任。'),
            style: TextStyle(fontSize: 13, height: 1.6),
          ),
        ),
        actions: [
          TextButton.icon(
            onPressed: () async {
              await Clipboard.setData(const ClipboardData(text: email));
              if (ctx.mounted) Navigator.pop(ctx);
              if (mounted) {
                ScaffoldMessenger.of(context).showSnackBar(
                    const SnackBar(content: Text('已复制 $email')));
              }
            },
            icon: const Icon(Icons.copy, size: 16),
            label: const Text('复制'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('关闭'),
          ),
        ],
      ),
    );
  }

  Widget _infoCard({required String title, required List<List<String>> rows}) {
    return Container(
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
          ...rows.map((r) => Padding(
                padding: const EdgeInsets.symmetric(vertical: 4),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    SizedBox(width: 160, child: Text(r[0], style: TextStyle(fontSize: 13, color: AppTheme.textSecondary))),
                    Expanded(child: SelectableText(r[1], style: TextStyle(fontSize: 13, color: AppTheme.textPrimary))),
                  ],
                ),
              )),
        ],
      ),
    );
  }

  Widget _usageBar({
    required String title,
    required double total,
    required List<_Seg> segments,
    required String totalText,
  }) {
    final sum = segments.fold<double>(0, (a, b) => a + (b.value > 0 ? b.value : 0));
    final denom = total > 0 ? total : (sum > 0 ? sum : 1);
    String pct(double v) => denom <= 0 ? '0%' : '${(v / denom * 100).toStringAsFixed(1)}%';

    return Padding(
      padding: const EdgeInsets.only(bottom: 20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(title, style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: AppTheme.textPrimary)),
              const Spacer(),
              Text(t('总量 $totalText'), style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
            ],
          ),
          const SizedBox(height: 8),
          ClipRRect(
            borderRadius: BorderRadius.circular(6),
            child: SizedBox(
              height: 16,
              child: Row(
                children: segments.map((s) {
                  final flex = (s.value <= 0 ? 0.0 : (s.value / denom * 1000)).round();
                  if (flex <= 0) return const SizedBox.shrink();
                  return Expanded(flex: flex, child: Container(color: s.color));
                }).toList(),
              ),
            ),
          ),
          const SizedBox(height: 6),
          Text(
            segments.map((s) => '${s.label} ${pct(s.value)}').join('   ·   '),
            style: TextStyle(fontSize: 12, color: AppTheme.textSecondary),
          ),
        ],
      ),
    );
  }
}

class _Seg {
  final String label;
  final double value;
  final Color color;
  _Seg(this.label, this.value, this.color);
}

_Seg _seg(String label, double value, Color color) => _Seg(label, value <= 0 ? 0 : value, color);
