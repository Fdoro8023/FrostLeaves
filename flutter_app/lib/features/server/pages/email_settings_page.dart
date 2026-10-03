import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';

/// 服务端「邮箱注册」页（模块 5 UI 入口）。
/// 上半部分：SMTP 服务器配置 + 预设一键填充；下半部分：邮箱验证码登录/注册自测。
class EmailSettingsPage extends ConsumerStatefulWidget {
  const EmailSettingsPage({super.key});

  @override
  ConsumerState<EmailSettingsPage> createState() => _EmailSettingsPageState();
}

class _EmailSettingsPageState extends ConsumerState<EmailSettingsPage> {
  bool _loading = true;
  List<Map<String, dynamic>> _presets = [];
  String _provider = 'custom';
  bool _enabled = false;
  String _security = 'ssl';

  final _host = TextEditingController();
  final _port = TextEditingController();
  final _username = TextEditingController();
  final _password = TextEditingController();
  final _fromName = TextEditingController();
  final _fromEmail = TextEditingController();
  final _subject = TextEditingController();
  final _body = TextEditingController();
  final _codeLen = TextEditingController();
  final _codeTtl = TextEditingController();
  final _perMinute = TextEditingController();
  final _perHour = TextEditingController();
  final _ipPerMinute = TextEditingController();

  // 邮箱验证码自测
  final _testEmail = TextEditingController();
  final _testCode = TextEditingController();
  final _captchaInput = TextEditingController();
  String? _captchaId;
  String? _captchaImage;

  bool get _isNewPassword => _password.text.trim().isEmpty;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    for (final c in [
      _host, _port, _username, _password, _fromName, _fromEmail, _subject, _body,
      _codeLen, _codeTtl, _perMinute, _perHour, _ipPerMinute,
      _testEmail, _testCode, _captchaInput,
    ]) {
      c.dispose();
    }
    super.dispose();
  }

  void _snack(String msg) {
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(msg)));
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    final api = ref.read(apiServiceProvider);
    try {
      final cfg = await api.getEmailConfig();
      final providers = await api.getEmailProviders();
      final data = (cfg['data'] as Map?)?.cast<String, dynamic>() ?? {};
      final all = (providers['data']?['all'] as List?) ?? [];
      if (!mounted) return;
      setState(() {
        _presets = all.map((e) => (e as Map).cast<String, dynamic>()).toList();
        _enabled = data['enabled'] == true;
        _provider = (data['provider'] ?? 'custom').toString();
        _security = (data['security'] ?? 'ssl').toString();
        _host.text = (data['host'] ?? '').toString();
        _port.text = '${data['port'] ?? ''}';
        _username.text = (data['username'] ?? '').toString();
        _password.clear();
        _fromName.text = (data['from_name'] ?? '').toString();
        _fromEmail.text = (data['from_email'] ?? '').toString();
        _subject.text = (data['subject_template'] ?? '').toString();
        _body.text = (data['body_template'] ?? '').toString();
        _codeLen.text = '${data['code_length'] ?? 6}';
        _codeTtl.text = '${data['code_ttl_minutes'] ?? 10}';
        _perMinute.text = '${data['email_per_minute'] ?? 1}';
        _perHour.text = '${data['email_per_hour'] ?? 5}';
        _ipPerMinute.text = '${data['ip_per_minute'] ?? 3}';
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _loading = false);
      _snack(t('加载失败: ${ApiService.describeError(e)}'));
    }
  }

  Future<void> _applyPreset(String key) async {
    try {
      final resp = await ref.read(apiServiceProvider).applyEmailPreset(key);
      final data = (resp['data'] as Map?)?.cast<String, dynamic>() ?? {};
      setState(() {
        _provider = (data['provider'] ?? key).toString();
        _host.text = (data['host'] ?? '').toString();
        _port.text = '${data['port'] ?? ''}';
        _security = (data['security'] ?? 'ssl').toString();
      });
      _snack(t('已套用预设，请补充账号与授权码'));
    } catch (e) {
      _snack(t('套用预设失败: ${ApiService.describeError(e)}'));
    }
  }

  Future<void> _save() async {
    final cfg = <String, dynamic>{
      'enabled': _enabled,
      'provider': _provider,
      'host': _host.text.trim(),
      'port': int.tryParse(_port.text.trim()) ?? 0,
      'security': _security,
      'username': _username.text.trim(),
      'password': _password.text,
      'from_name': _fromName.text.trim(),
      'from_email': _fromEmail.text.trim(),
      'subject_template': _subject.text.trim(),
      'body_template': _body.text.trim(),
      'code_length': int.tryParse(_codeLen.text.trim()) ?? 6,
      'code_ttl_minutes': int.tryParse(_codeTtl.text.trim()) ?? 10,
      'email_per_minute': int.tryParse(_perMinute.text.trim()) ?? 1,
      'email_per_hour': int.tryParse(_perHour.text.trim()) ?? 5,
      'ip_per_minute': int.tryParse(_ipPerMinute.text.trim()) ?? 3,
    };
    try {
      await ref.read(apiServiceProvider).setEmailConfig(cfg);
      _snack(t('邮箱配置已保存'));
      _load();
    } catch (e) {
      _snack(t('保存失败: ${ApiService.describeError(e)}'));
    }
  }

  Future<void> _refreshCaptcha() async {
    try {
      final resp = await ref.read(apiServiceProvider).getCaptcha();
      final data = (resp['data'] as Map?)?.cast<String, dynamic>() ?? {};
      setState(() {
        _captchaId = (data['id'] ?? '').toString();
        _captchaImage = (data['image'] ?? '').toString();
      });
    } catch (e) {
      _snack(t('获取验证码失败: ${ApiService.describeError(e)}'));
    }
  }

  Future<void> _requestCode() async {
    if (_captchaId == null) {
      _snack(t('请先获取图形验证码'));
      return;
    }
    try {
      await ref.read(apiServiceProvider).requestEmailCode(
            _captchaId!, _captchaInput.text.trim(), _testEmail.text.trim());
      _snack(t('验证码已发送（如已启用）'));
      _refreshCaptcha();
    } catch (e) {
      _snack(t('发送失败: ${ApiService.describeError(e)}'));
      _refreshCaptcha();
    }
  }

  Future<void> _verifyCode() async {
    try {
      await ref.read(apiServiceProvider)
          .verifyEmailCode(_testEmail.text.trim(), _testCode.text.trim());
      _snack(t('邮箱验证成功 ✅'));
    } catch (e) {
      _snack(t('验证失败: ${ApiService.describeError(e)}'));
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
              Text(t('邮箱注册'),
                  style: TextStyle(
                      fontSize: 24, fontWeight: FontWeight.bold, color: AppTheme.textPrimary)),
              const Spacer(),
              IconButton(icon: const Icon(Icons.refresh), tooltip: t('刷新'), onPressed: _load),
            ],
          ),
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
                              Text(t('SMTP 服务器'),
                                  style: TextStyle(
                                      fontWeight: FontWeight.w700, color: AppTheme.textPrimary)),
                              const SizedBox(height: 8),
                              SwitchListTile(
                                contentPadding: EdgeInsets.zero,
                                dense: true,
                                title: Text(t('启用邮箱注册/验证码登录')),
                                subtitle: Text(t('关闭时公开的验证码接口会返回 403'),
                                    style: const TextStyle(fontSize: 12)),
                                value: _enabled,
                                onChanged: (v) => setState(() => _enabled = v),
                              ),
                              const SizedBox(height: 8),
                              Wrap(
                                spacing: 8,
                                runSpacing: 4,
                                children: _presets
                                    .map((p) => ActionChip(
                                          label: Text('${p['name']}'),
                                          onPressed: () => _applyPreset('${p['key']}'),
                                        ))
                                    .toList(),
                              ),
                              const SizedBox(height: 12),
                              Row(
                                children: [
                                  Expanded(child: _tf(_host, t('SMTP 主机'))),
                                  const SizedBox(width: 12),
                                  SizedBox(width: 110, child: _tf(_port, t('端口'))),
                                  const SizedBox(width: 12),
                                  SizedBox(
                                    width: 150,
                                    child: DropdownButtonFormField<String>(
                                      value: _security,
                                      decoration: InputDecoration(
                                          labelText: t('加密方式'),
                                          isDense: true,
                                          border: const OutlineInputBorder()),
                                      items: const [
                                        DropdownMenuItem(value: 'ssl', child: Text('SSL/TLS')),
                                        DropdownMenuItem(value: 'starttls', child: Text('STARTTLS')),
                                        DropdownMenuItem(value: 'none', child: Text('无')),
                                      ],
                                      onChanged: (v) => setState(() => _security = v ?? 'ssl'),
                                    ),
                                  ),
                                ],
                              ),
                              const SizedBox(height: 12),
                              Row(
                                children: [
                                  Expanded(child: _tf(_username, t('登录账号'))),
                                  const SizedBox(width: 12),
                                  Expanded(
                                    child: _tf(_password,
                                        _isNewPassword ? t('授权码 / 密码') : t('授权码（留空=不修改）'),
                                        obscure: true),
                                  ),
                                ],
                              ),
                              const SizedBox(height: 12),
                              Row(
                                children: [
                                  Expanded(child: _tf(_fromName, t('发件人名称'))),
                                  const SizedBox(width: 12),
                                  Expanded(child: _tf(_fromEmail, t('发件邮箱'))),
                                ],
                              ),
                            ],
                          ),
                        ),
                      ),
                      const SizedBox(height: 12),
                      Card(
                        child: Padding(
                          padding: const EdgeInsets.all(16),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(t('邮件模板与限流'),
                                  style: TextStyle(
                                      fontWeight: FontWeight.w700, color: AppTheme.textPrimary)),
                              const SizedBox(height: 12),
                              _tf(_subject, t('邮件标题模板')),
                              const SizedBox(height: 12),
                              _tf(_body, t('邮件正文模板（{code} / {ttl}）'), maxLines: 3),
                              const SizedBox(height: 12),
                              Row(
                                children: [
                                  Expanded(child: _tf(_codeLen, t('验证码位数'))),
                                  const SizedBox(width: 12),
                                  Expanded(child: _tf(_codeTtl, t('有效期 (分钟)'))),
                                ],
                              ),
                              const SizedBox(height: 12),
                              Row(
                                children: [
                                  Expanded(child: _tf(_perMinute, t('每邮箱/分钟'))),
                                  const SizedBox(width: 12),
                                  Expanded(child: _tf(_perHour, t('每邮箱/小时'))),
                                  const SizedBox(width: 12),
                                  Expanded(child: _tf(_ipPerMinute, t('每 IP/分钟'))),
                                ],
                              ),
                            ],
                          ),
                        ),
                      ),
                      const SizedBox(height: 16),
                      Row(
                        children: [
                          ElevatedButton.icon(
                            onPressed: _save,
                            icon: const Icon(Icons.save, size: 18),
                            label: Text(t('保存邮箱配置')),
                          ),
                        ],
                      ),
                      const SizedBox(height: 24),
                      Card(
                        child: Padding(
                          padding: const EdgeInsets.all(16),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(t('邮箱验证码自测'),
                                  style: TextStyle(
                                      fontWeight: FontWeight.w700, color: AppTheme.textPrimary)),
                              const SizedBox(height: 4),
                              Text(t('用于验证配置是否可用：取图形验证码 → 发送邮箱验证码 → 校验。'),
                                  style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
                              const SizedBox(height: 12),
                              Row(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Expanded(child: _tf(_testEmail, t('邮箱地址'))),
                                  const SizedBox(width: 12),
                                  Expanded(child: _tf(_captchaInput, t('图形验证码'))),
                                  const SizedBox(width: 12),
                                  Column(
                                    children: [
                                      Container(
                                        width: 120,
                                        height: 40,
                                        decoration: BoxDecoration(
                                          color: AppTheme.surfaceColor,
                                          borderRadius: BorderRadius.circular(6),
                                          border: Border.all(color: AppTheme.borderColor),
                                        ),
                                        child: _captchaImage == null
                                            ? Center(
                                                child: Text(t('未获取'),
                                                    style: TextStyle(
                                                        fontSize: 12,
                                                        color: AppTheme.textSecondary)))
                                            : Image.memory(
                                                _decode(_captchaImage!),
                                                fit: BoxFit.contain,
                                                errorBuilder: (_, __, ___) =>
                                                    const SizedBox.shrink(),
                                              ),
                                      ),
                                      TextButton(
                                        onPressed: _refreshCaptcha,
                                        child: Text(t('换一张'), style: const TextStyle(fontSize: 12)),
                                      ),
                                    ],
                                  ),
                                ],
                              ),
                              const SizedBox(height: 8),
                              Wrap(
                                spacing: 12,
                                children: [
                                  OutlinedButton.icon(
                                    onPressed: _requestCode,
                                    icon: const Icon(Icons.mail_outline, size: 18),
                                    label: Text(t('发送验证码')),
                                  ),
                                  Row(
                                    children: [
                                      SizedBox(width: 140, child: _tf(_testCode, t('邮箱验证码'))),
                                      const SizedBox(width: 8),
                                      OutlinedButton(
                                        onPressed: _verifyCode,
                                        child: Text(t('校验')),
                                      ),
                                    ],
                                  ),
                                ],
                              ),
                            ],
                          ),
                        ),
                      ),
                      const SizedBox(height: 24),
                    ],
                  ),
          ),
        ],
      ),
    );
  }

  Uint8List _decode(String dataUrl) {
    final idx = dataUrl.indexOf('base64,');
    final b64 = idx >= 0 ? dataUrl.substring(idx + 7) : dataUrl;
    return base64Decode(b64);
  }

  Widget _tf(TextEditingController c, String label, {bool obscure = false, int maxLines = 1}) {
    return TextField(
      controller: c,
      obscureText: obscure,
      maxLines: maxLines,
      decoration: InputDecoration(
        labelText: label,
        isDense: true,
        border: const OutlineInputBorder(),
      ),
    );
  }
}
