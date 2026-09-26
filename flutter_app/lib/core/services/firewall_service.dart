import '../../core/i18n/i18n.dart';
import 'dart:io';

/// Windows 防火墙放行服务（需求 6）
///
/// 方案 1：自动创建入站规则（通过 UAC 提权执行 netsh）
/// 方案 2：失败时给出完整的手动配置步骤（见 manualGuide）
class FirewallRuleResult {
  final int port;
  final bool ok;
  final String message;
  FirewallRuleResult({required this.port, required this.ok, required this.message});
}

class FirewallService {
  static const String rulePrefix = 'FrostLeaves';

  static String ruleName(int port) => '$rulePrefix-$port';

  /// 规则是否已存在
  static Future<bool> ruleExists(int port) async {
    if (!Platform.isWindows) return false;
    try {
      final r = await Process.run('netsh', [
        'advfirewall', 'firewall', 'show', 'rule', 'name=${ruleName(port)}',
      ]);
      return r.exitCode == 0;
    } catch (_) {
      return false;
    }
  }

  /// 放行单个端口（自动尝试提权；失败返回原因）
  static Future<FirewallRuleResult> ensureRule(int port) async {
    if (!Platform.isWindows) {
      return FirewallRuleResult(port: port, ok: false, message: t('仅 Windows 需要配置防火墙'));
    }
    if (await ruleExists(port)) {
      return FirewallRuleResult(port: port, ok: true, message: t('规则已存在：${ruleName(port)}'));
    }

    final netshArgs = [
      'advfirewall', 'firewall', 'add', 'rule',
      'name=${ruleName(port)}',
      'dir=in', 'action=allow', 'protocol=TCP',
      'localport=$port',
      'remoteip=any',
    ];
    final psList = netshArgs.map((a) => "'$a'").join(',');
    final command = 'Start-Process -Verb RunAs -Wait -FilePath netsh -ArgumentList $psList';

    try {
      final r = await Process.run('powershell', ['-NoProfile', '-Command', command]);
      final ok = await ruleExists(port);
      if (ok) {
        return FirewallRuleResult(port: port, ok: true, message: t('已放行端口 $port'));
      }
      final err = (r.stderr.toString().trim().isNotEmpty)
          ? r.stderr.toString().trim()
          : r.stdout.toString().trim();
      return FirewallRuleResult(
        port: port,
        ok: false,
        message: err.isEmpty ? t('未创建成功（可能取消了 UAC 授权）') : err,
      );
    } catch (e) {
      return FirewallRuleResult(port: port, ok: false, message: '$e');
    }
  }

  /// 批量放行
  static Future<List<FirewallRuleResult>> ensurePorts(List<int> ports) async {
    final List<FirewallRuleResult> results = [];
    for (final p in ports) {
      results.add(await ensureRule(p));
    }
    return results;
  }

  /// 方案 2：手动配置指引文本（可直接复制）
  static String manualGuide(List<int> ports) {
    final buf = StringBuffer();
    buf.writeln(t('【方式一：命令行】以管理员身份打开 PowerShell 或 CMD，逐条执行：'));
    buf.writeln('');
    for (final p in ports) {
      buf.writeln(
          'netsh advfirewall firewall add rule name="${ruleName(p)}" dir=in action=allow protocol=TCP localport=$p remoteip=any');
    }
    buf.writeln('');
    buf.writeln(t('【方式二：图形界面】'));
    buf.writeln(t('1. 按 Win+R，输入 wf.msc 回车'));
    buf.writeln(t('2. 左侧点「入站规则」→ 右侧「新建规则」'));
    buf.writeln(t('3. 规则类型选「端口」→ 下一步'));
    buf.writeln(t('4. 选「TCP」，特定本地端口填：${ports.join(",")}'));
    buf.writeln(t('5. 选「允许连接」→ 下一步（域/专用/公用全勾选）'));
    buf.writeln(t('6. 名称填 FrostLeaves → 完成'));
    buf.writeln('');
    buf.writeln(t('【检查是否生效】'));
    buf.writeln('netsh advfirewall firewall show rule name=all | findstr FrostLeaves');
    return buf.toString();
  }
}
