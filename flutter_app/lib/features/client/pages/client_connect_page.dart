import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:uuid/uuid.dart';
import 'package:dio/dio.dart';
import 'package:go_router/go_router.dart';
import 'package:path_provider/path_provider.dart';
import 'package:share_plus/share_plus.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';
import '../../../core/services/local_storage_service.dart';
import '../../../core/widgets/floating_message.dart';
import '../../../core/services/connection_state.dart';

enum _ConnectState { idle, applying, waitingApproval, verifying, connected, error, emailLogin }

class ClientConnectPage extends ConsumerStatefulWidget {
  const ClientConnectPage({super.key});
  @override
  ConsumerState<ClientConnectPage> createState() => _ClientConnectPageState();
}

class _ClientConnectPageState extends ConsumerState<ClientConnectPage> {
  final _serverUrlController = TextEditingController();
  final _codeController = TextEditingController();
  final _emailController = TextEditingController();
  final _emailCodeController = TextEditingController();
  final _captchaInputController = TextEditingController();
  String? _captchaId;
  String? _captchaImage;
  bool _showCodeEntry = false;
  _ConnectState _state = _ConnectState.idle;
  String _deviceId = '';
  String _errorMsg = '';
  String _serverName = '';
  bool _isLoading = true;
  Timer? _statusTimer;

  @override
  void initState() {
    super.initState();
    _deviceId = const Uuid().v4().substring(0, 8);
    _loadSavedConfig();
  }

  @override
  void dispose() {
    _statusTimer?.cancel();
    _serverUrlController.dispose();
    _codeController.dispose();
    _emailController.dispose();
    _emailCodeController.dispose();
    _captchaInputController.dispose();
    super.dispose();
  }

  // ========== 需求 1：连接状态轮询 ==========

  void _startStatusPolling() {
    _statusTimer?.cancel();
    _statusTimer = Timer.periodic(const Duration(seconds: 4), (_) => _checkStatus());
  }

  void _stopStatusPolling() {
    _statusTimer?.cancel();
    _statusTimer = null;
  }

  /// 轮询设备状态：被拉黑/踢出/token 失效 → 显示【未连接】
  /// 拉取真实状态；manual=true 时返回可展示的结果文案（不再静默吞掉错误）
  Future<String?> _checkStatus({bool manual = false}) async {
    if (_state != _ConnectState.connected) {
      return manual ? t('当前未连接') : null;
    }
    try {
      final api = ref.read(apiServiceProvider);
      final resp = await api.getDeviceStatus(_deviceId);
      final data = resp['data'] as Map<String, dynamic>?;
      if (data == null) {
        return manual ? t('服务端未返回状态') : null;
      }

      final status = (data['status'] ?? 'unknown').toString();
      final tokenValid = data['token_valid'] == true;
      final name = (data['server_name'] ?? '').toString();

      final kicked = status == 'blacklisted' ||
          status == 'rejected' ||
          status == 'unknown' ||
          !tokenValid;

      if (!mounted) return null;
      if (name.isNotEmpty && name != _serverName) {
        setState(() => _serverName = name);
      }

      if (kicked) {
        _stopStatusPolling();
        ref.read(apiServiceProvider).clearCredentials();
        _setConn(false);
        setState(() {
          _state = _ConnectState.idle;
          _errorMsg = status == 'blacklisted' ? t('已被服务端拉黑，无法继续连接') : t('已被服务端断开连接');
          _deviceId = const Uuid().v4().substring(0, 8);
        });
        if (mounted) {
          FloatingMessage.show(
            context: context,
            title: t('连接已断开'),
            message: _errorMsg,
            type: MessageType.warning,
          );
        }
        return _errorMsg;
      }
      return t('已连接');
    } on DioException catch (e) {
      // 服务端已无该设备（400/401/403/404）→ 视为已被断开，如实反映
      final code = e.response?.statusCode ?? 0;
      if (manual && (code == 400 || code == 401 || code == 403 || code == 404)) {
        _stopStatusPolling();
        ref.read(apiServiceProvider).clearCredentials();
        _setConn(false);
        if (mounted) {
          setState(() {
            _state = _ConnectState.idle;
            _errorMsg = t('服务端已无此设备，请重新申请连接');
            _deviceId = const Uuid().v4().substring(0, 8);
          });
        }
        return _errorMsg;
      }
      return manual ? t('刷新失败：${ApiService.describeError(e)}') : null;
    } catch (e) {
      return manual ? t('刷新失败：$e') : null;
    }
  }

  /// 需求 1：手动刷新（重新查询状态）
  Future<void> _manualRefresh() async {
    final msg = await _checkStatus(manual: true);
    if (!mounted) return;
    final ok = msg == t('已连接');
    // 拉取不通时如实标记未连接（避免显示绿色"刷新成功"却其实连不上）
    if (!ok && _state == _ConnectState.connected) {
      _stopStatusPolling();
      _setConn(false);
      setState(() {
        _state = _ConnectState.idle;
        _errorMsg = msg ?? t('无法连接服务端');
      });
    }
    FloatingMessage.show(
      context: context,
      title: ok ? t('刷新成功') : t('刷新失败'),
      message: msg ?? t('已刷新'),
      type: ok ? MessageType.success : MessageType.error,
    );
  }

  Future<void> _loadSavedConfig() async {
    try {
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();

      final savedUrl = await localStorageService.getServerUrl();
      if (savedUrl != null && savedUrl.isNotEmpty) {
        _serverUrlController.text = savedUrl;
      } else {
        _serverUrlController.text = 'http://192.168.1.100:9091';
      }

      final creds = await localStorageService.getCredentials();
      if (creds != null) {
        _deviceId = creds['deviceId']!;
        setState(() {
          _state = _ConnectState.connected;
          _isLoading = false;
        });
        _setConn(true);
        _startStatusPolling();
        unawaited(_checkStatus());
      } else {
        setState(() => _isLoading = false);
      }
    } catch (e) {
      print('Error loading config: $e');
      _serverUrlController.text = 'http://192.168.1.100:9091';
      setState(() => _isLoading = false);
    }
  }

  Future<void> _applyConnect() async {
    final serverUrl = _serverUrlController.text.trim();
    if (serverUrl.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(t('请输入服务端地址'))),
      );
      return;
    }

    setState(() { _state = _ConnectState.applying; _errorMsg = ''; });
    try {
      final api = ref.read(apiServiceProvider);
      api.setServerUrl(serverUrl);
      // HTTPS：首次需要从引导端口取回自签 CA 并信任
      final uri = Uri.tryParse(serverUrl);
      if (uri != null && uri.scheme == 'https' && !api.hasTrustedCa) {
        final ok = await api.bootstrapCa(uri.host);
        if (!ok) {
          setState(() {
            _state = _ConnectState.error;
            _errorMsg = t('无法获取服务器证书（CA）。请确认服务端已启用 HTTPS，且引导端口 9093 可达；或改用 http://（仅限可信网络）。');
          });
          return;
        }
      }
      // 需求 5：把设备型号一并上报
      try {
        api.setDeviceModel(await api.resolveDeviceModel());
      } catch (_) {}

      // 项目 4：先问服务端登录方式；需求 1：同时校验版本号
      bool forceEmail = false;
      try {
        final mode = await api.getLoginMode();
        final md = mode['data'] as Map<String, dynamic>?;
        forceEmail = md?['force_email_login'] == true;
        final sv = (md?['server_version'] ?? '').toString();
        if (sv.isNotEmpty && !_sameVersion(sv, ApiService.clientVersion)) {
          setState(() => _state = _ConnectState.error);
          await _showVersionMismatch(sv);
          return;
        }
      } catch (_) {
        // 旧版服务端没有该接口 → 退回设备验证流程
      }

      if (forceEmail) {
        setState(() => _state = _ConnectState.emailLogin);
        unawaited(_refreshCaptcha());
        return;
      }

      await api.applyDevice(_deviceId, 'My Device', _platformName());
      setState(() => _state = _ConnectState.waitingApproval);
    } catch (e) {
      if (ApiService.isVersionMismatch(e)) {
        setState(() => _state = _ConnectState.error);
        await _showVersionMismatch(ApiService.serverVersionFromError(e));
        return;
      }
      setState(() { _state = _ConnectState.error; _errorMsg = ApiService.describeError(e); });
    }
  }

  Future<void> _verifyCode() async {
    final code = _codeController.text.trim();
    if (code.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(t('请输入验证码'))),
      );
      return;
    }

    setState(() { _state = _ConnectState.verifying; _errorMsg = ''; });
    try {
      final api = ref.read(apiServiceProvider);
      final resp = await api.verifyCode(_deviceId, code);
      final data = resp['data'] as Map<String, dynamic>?;
      if (data != null && data['token'] != null) {
        api.setCredentials(_deviceId, data['token'] as String);
      }
      setState(() => _state = _ConnectState.connected);
      _setConn(true);
      _startStatusPolling();
      unawaited(_checkStatus());
    } catch (e) {
      setState(() { _state = _ConnectState.error; _errorMsg = ApiService.describeError(e); });
    }
  }

  String _platformName() => Platform.isWindows
      ? 'windows'
      : (Platform.isAndroid
          ? 'android'
          : (Platform.isMacOS
              ? 'macos'
              : (Platform.isLinux ? 'linux' : Platform.operatingSystem)));

  /// 需求 1：版本归一化（去 v 前缀/空格/大小写）
  static String _normVersion(String v) =>
      v.trim().replaceAll(RegExp(r'^[vV]'), '').replaceAll(' ', '').toLowerCase();

  static bool _sameVersion(String a, String b) => _normVersion(a) == _normVersion(b);

  /// 需求 1：版本不匹配弹窗（拒绝连接并提示更新）
  Future<void> _showVersionMismatch(String serverVersion) async {
    if (!mounted) return;
    await showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t('版本不匹配')),
        content: Text(
          '${t('客户端版本与服务端不一致，无法连接。')}\n'
          '${t('客户端版本')}: ${ApiService.clientVersion}\n'
          '${t('服务端版本')}: ${serverVersion.isEmpty ? t('未知') : serverVersion}\n\n'
          '${t('请把客户端更新到与服务端一致的版本。')}',
          style: const TextStyle(height: 1.5),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: Text(t('知道了'))),
        ],
      ),
    );
  }

  void _snack(String msg) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(msg)));
  }

  /// 项目 4①：等待审核阶段手动查询审核状态。
  /// 返回：待审核 / 审核通过 / 拒绝；审核通过则免验证码直接接入。
  Future<void> _queryApprovalStatus() async {
    try {
      final api = ref.read(apiServiceProvider);
      final resp = await api.getDeviceStatus(_deviceId);
      final data = resp['data'] as Map<String, dynamic>?;
      final status = (data?['status'] ?? 'unknown').toString();
      if (!mounted) return;
      switch (status) {
        case 'pending':
          FloatingMessage.show(
            context: context,
            title: t('审核状态'),
            message: t('待审核：管理员尚未处理，请稍候'),
            type: MessageType.warning,
          );
          return;
        case 'rejected':
        case 'blacklisted':
          setState(() {
            _state = _ConnectState.error;
            _errorMsg = status == 'blacklisted' ? t('已被服务端拉黑') : t('管理员已拒绝该设备');
          });
          _deviceId = const Uuid().v4().substring(0, 8);
          return;
        case 'approved':
        case 'connected':
        case 'disconnected':
          // 审核通过 → 免验证码直接接入
          final resp2 = await api.connectDevice(_deviceId);
          final d2 = resp2['data'] as Map<String, dynamic>?;
          if (d2 != null && d2['token'] != null) {
            api.setCredentials(_deviceId, d2['token'] as String);
          }
          if (!mounted) return;
          setState(() => _state = _ConnectState.connected);
          _setConn(true);
          _startStatusPolling();
          unawaited(_checkStatus());
          if (mounted) {
            FloatingMessage.show(
              context: context,
              title: t('审核已通过'),
              message: t('已直接接入服务器'),
              type: MessageType.success,
            );
          }
          return;
        default:
          FloatingMessage.show(
            context: context,
            title: t('审核状态'),
            message: t('服务端未返回该设备，请重新申请连接'),
            type: MessageType.error,
          );
      }
    } catch (e) {
      if (!mounted) return;
      FloatingMessage.show(
        context: context,
        title: t('查询失败'),
        message: ApiService.describeError(e),
        type: MessageType.error,
      );
    }
  }

  // ===== 项目 4②：邮箱验证码登录 / 注册 =====

  Future<void> _refreshCaptcha() async {
    try {
      final api = ref.read(apiServiceProvider);
      api.setServerUrl(_serverUrlController.text.trim());
      final resp = await api.getCaptcha();
      final data = resp['data'] as Map<String, dynamic>?;
      if (!mounted) return;
      setState(() {
        _captchaId = (data?['id'] ?? '').toString();
        _captchaImage = (data?['image'] ?? '').toString();
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _state = _ConnectState.error;
        _errorMsg = ApiService.describeError(e);
      });
    }
  }

  Future<void> _requestEmailCode() async {
    final email = _emailController.text.trim();
    if (email.isEmpty) {
      _snack(t('请输入邮箱地址'));
      return;
    }
    if (_captchaId == null || _captchaId!.isEmpty) {
      _snack(t('请先获取图形验证码'));
      return;
    }
    try {
      final api = ref.read(apiServiceProvider);
      api.setServerUrl(_serverUrlController.text.trim());
      await api.requestEmailCode(
          _captchaId!, _captchaInputController.text.trim(), email);
      if (!mounted) return;
      FloatingMessage.show(
        context: context,
        title: t('验证码已发送'),
        message: t('请查收邮件并填写验证码'),
        type: MessageType.success,
      );
      unawaited(_refreshCaptcha());
    } catch (e) {
      if (!mounted) return;
      FloatingMessage.show(
        context: context,
        title: t('发送失败'),
        message: ApiService.describeError(e),
        type: MessageType.error,
      );
      unawaited(_refreshCaptcha());
    }
  }

  Future<void> _doEmailLogin() async {
    final email = _emailController.text.trim();
    final code = _emailCodeController.text.trim();
    if (email.isEmpty || code.isEmpty) {
      _snack(t('请输入邮箱地址和验证码'));
      return;
    }
    setState(() {
      _state = _ConnectState.verifying;
      _errorMsg = '';
    });
    try {
      final api = ref.read(apiServiceProvider);
      api.setServerUrl(_serverUrlController.text.trim());
      final resp = await api.emailLogin(
        email: email,
        code: code,
        deviceId: _deviceId,
        deviceName: 'My Device',
        platform: _platformName(),
      );
      final data = resp['data'] as Map<String, dynamic>?;
      final token = data?['token'] as String?;
      if (token != null && token.isNotEmpty) {
        // 兼容返回令牌的服务端：直接接入
        api.setCredentials(_deviceId, token);
        if (!mounted) return;
        setState(() => _state = _ConnectState.connected);
        _setConn(true);
        _startStatusPolling();
        unawaited(_checkStatus());
        return;
      }
      // 邮箱验证通过 ≠ 放行：仍需管理员在【设备管理-待审核】审核通过
      if (!mounted) return;
      setState(() => _state = _ConnectState.waitingApproval);
      FloatingMessage.show(
        context: context,
        title: t('邮箱验证成功'),
        message: t('设备已提交，等待管理员审核通过后即可接入'),
        type: MessageType.success,
      );
    } catch (e) {
      if (ApiService.isVersionMismatch(e)) {
        setState(() => _state = _ConnectState.error);
        await _showVersionMismatch(ApiService.serverVersionFromError(e));
        return;
      }
      setState(() {
        _state = _ConnectState.error;
        _errorMsg = ApiService.describeError(e);
      });
    }
  }

  Future<void> _disconnect() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t('确认退出')),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(t('确定要退出当前服务器吗？退出后需要重新验证才能连接。')),
            const SizedBox(height: 12),
            Container(
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: const Color(0xFFFFF4CE),
                border: Border.all(color: const Color(0xFFFFC107)),
                borderRadius: BorderRadius.circular(8),
              ),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Icon(Icons.warning_amber_rounded,
                      color: Color(0xFFB26A00), size: 20),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      t('警告：本软件没有账户机制，退出服务器后，你在该服务器上的所有私人文件都会丢失且无法找回。请务必先自行备份重要文件！'),
                      style: const TextStyle(
                          color: Color(0xFF8A5A00), fontSize: 13, height: 1.4),
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text(t('取消')),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(t('退出'), style: TextStyle(color: AppTheme.errorColor)),
          ),
        ],
      ),
    );

    if (confirmed != true) return;
    _stopStatusPolling();

    try {
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();
      var released = false;
      try {
        await api.releaseDevice(); // 服务端撤销令牌并移除设备 → 计数随之变化
        released = true;
      } catch (_) {}
      api.clearCredentials();
      _setConn(false);
      setState(() {
        _state = _ConnectState.idle;
        _deviceId = const Uuid().v4().substring(0, 8);
      });
      if (mounted) {
        FloatingMessage.show(
          context: context,
          title: released ? t('已退出服务器') : t('已在本地退出'),
          message: released
              ? t('服务端已移除本设备，可重新申请连接')
              : t('未能通知服务端，服务端可能仍显示在线；请稍后重试或让管理员踢出'),
          type: released ? MessageType.success : MessageType.warning,
        );
      }
    } catch (e) {
      final api = ref.read(apiServiceProvider);
      api.clearCredentials();
      _setConn(false);
      setState(() {
        _state = _ConnectState.idle;
        _deviceId = const Uuid().v4().substring(0, 8);
      });
    }
  }

  Future<void> _generateLogs() async {
    try {
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();

      final tempDir = await getTemporaryDirectory();
      final logFile = File('${tempDir.path}/frostleaves_logs_${DateTime.now().millisecondsSinceEpoch}.txt');

      final logContent = StringBuffer();
      logContent.writeln(t('=== Frost Leaves 客户端日志 ==='));
      logContent.writeln(t('生成时间：${DateTime.now().toIso8601String()}'));
      logContent.writeln('');
      logContent.writeln(t('=== 设备信息 ==='));
      logContent.writeln(t('设备 ID: $_deviceId'));
      logContent.writeln(t('平台: ${Platform.operatingSystem}'));
      logContent.writeln('');
      logContent.writeln(t('=== 连接信息 ==='));
      logContent.writeln(t('服务端地址：${api.authUrl}'));
      logContent.writeln(t('文件网关：${api.fileUrl}'));
      logContent.writeln(t('服务端名称：$_serverName'));
      logContent.writeln(t('Token: ${api.token != null ? "已设置" : "未设置"}'));
      logContent.writeln('');
      logContent.writeln(t('=== 最近错误 ==='));
      logContent.writeln(_errorMsg.isNotEmpty ? _errorMsg : t('无错误记录'));
      logContent.writeln('');
      logContent.writeln(t('=== 连接状态 ==='));
      logContent.writeln(t('当前状态：$_state'));

      await logFile.writeAsString(logContent.toString());

      if (mounted) {
        await Share.shareXFiles(
          [XFile(logFile.path)],
          subject: t('Frost Leaves 客户端日志'),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t('生成日志失败：$e'))),
        );
      }
    }
  }

  bool get _isConnected => _state == _ConnectState.connected;

  /// 同步全局连接状态，令外壳/状态区一起刷新
  void _setConn(bool v) {
    if (ref.read(connectionStateProvider) != v) {
      ref.read(connectionStateProvider.notifier).state = v;
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_isLoading) {
      return const Center(child: CircularProgressIndicator());
    }

    return SingleChildScrollView(
      padding: const EdgeInsets.all(24),
      child: Center(
        child: SizedBox(
          width: 480,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.center,
            children: [
              // ===== 需求 1：左上角实时连接状态 ===== 
              Align(
                alignment: Alignment.centerLeft,
                child: Container(
                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                  decoration: BoxDecoration(
                    color: AppTheme.cardColor,
                    borderRadius: BorderRadius.circular(999),
                    border: Border.all(color: AppTheme.borderColor),
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Container(
                        width: 8,
                        height: 8,
                        decoration: BoxDecoration(
                          color: _isConnected ? AppTheme.successColor : AppTheme.errorColor,
                          shape: BoxShape.circle,
                        ),
                      ),
                      const SizedBox(width: 8),
                      Text(
                        _isConnected ? t('已连接') : t('未连接'),
                        style: TextStyle(
                          fontSize: 13,
                          fontWeight: FontWeight.w600,
                          color: _isConnected ? AppTheme.successColor : AppTheme.errorColor,
                        ),
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 24),
              // ===== 屏幕中部状态 ===== 
              Container(
                width: 80, height: 80,
                decoration: BoxDecoration(
                  color: _stateColor.withOpacity(0.15),
                  shape: BoxShape.circle,
                ),
                child: Icon(_stateIcon, size: 40, color: _stateColor),
              ),
              const SizedBox(height: 20),
              Text(
                _isConnected ? t('已连接') : (_state == _ConnectState.idle ? t('未连接') : _stateText),
                style: TextStyle(fontSize: 20, fontWeight: FontWeight.w600, color: _stateColor),
              ),
              const SizedBox(height: 8),
              // ===== 需求 2：服务器名（自定义，默认「服务器」）===== 
              Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  Icon(Icons.dns_outlined, size: 16, color: AppTheme.textSecondary),
                  const SizedBox(width: 6),
                  Text(
                    _serverName.isNotEmpty ? _serverName : (_isConnected ? t('服务器') : t('未连接服务器')),
                    style: TextStyle(fontSize: 14, color: AppTheme.textPrimary),
                  ),
                ],
              ),
              const SizedBox(height: 6),
              Text(
                t('设备 ID: $_deviceId'),
                style: TextStyle(fontSize: 13, color: AppTheme.textSecondary),
              ),
              const SizedBox(height: 32),

              if (_state == _ConnectState.idle || _state == _ConnectState.error) ...[
                TextField(
                  controller: _serverUrlController,
                  decoration: InputDecoration(
                    labelText: t('服务端地址'),
                    hintText: 'http://192.168.1.100:9091',
                    prefixIcon: Icon(Icons.dns_outlined),
                  ),
                ),
                const SizedBox(height: 20),
                SizedBox(
                  width: double.infinity,
                  child: ElevatedButton(
                    onPressed: _applyConnect,
                    child: Text(t('发起连接申请')),
                  ),
                ),
              ],

              if (_state == _ConnectState.waitingApproval) ...[
                Container(
                  padding: const EdgeInsets.all(20),
                  decoration: BoxDecoration(
                    color: AppTheme.cardColor,
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(color: AppTheme.borderColor),
                  ),
                  child: Column(
                    children: [
                      Icon(Icons.hourglass_top, size: 40, color: AppTheme.warningColor),
                      const SizedBox(height: 12),
                      Text(t('等待管理员审批...'), style: TextStyle(color: AppTheme.textPrimary)),
                      const SizedBox(height: 6),
                      Text(t('管理员在「设备管理-待审核」中通过后，点击下方按钮查询即可直接接入'),
                          textAlign: TextAlign.center,
                          style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
                      const SizedBox(height: 16),
                      SizedBox(
                        width: double.infinity,
                        child: ElevatedButton.icon(
                          onPressed: _queryApprovalStatus,
                          icon: const Icon(Icons.fact_check_outlined, size: 18),
                          label: Text(t('审核状态查询')),
                        ),
                      ),
                      const SizedBox(height: 8),
                      TextButton(
                        onPressed: () =>
                            setState(() => _showCodeEntry = !_showCodeEntry),
                        child: Text(_showCodeEntry ? t('收起验证码') : t('改用验证码接入')),
                      ),
                      if (_showCodeEntry) ...[
                        const SizedBox(height: 4),
                        TextField(
                          controller: _codeController,
                          decoration: InputDecoration(
                            labelText: t('输入验证码'),
                            hintText: t('8 位验证码'),
                            prefixIcon: Icon(Icons.key),
                          ),
                          textAlign: TextAlign.center,
                          maxLength: 8,
                        ),
                        const SizedBox(height: 8),
                        SizedBox(
                          width: double.infinity,
                          child: ElevatedButton(
                            onPressed: _verifyCode,
                            child: Text(t('验证并连接')),
                          ),
                        ),
                      ],
                      const SizedBox(height: 8),
                      TextButton(
                        onPressed: () => setState(() => _state = _ConnectState.idle),
                        child: Text(t('取消')),
                      ),
                    ],
                  ),
                ),
              ],

              // ===== 项目 4②：强制邮箱登录 =====
              if (_state == _ConnectState.emailLogin) ...[
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
                      Row(
                        children: [
                          Icon(Icons.mark_email_read_outlined,
                              size: 22, color: AppTheme.primaryColor),
                          const SizedBox(width: 8),
                          Expanded(
                            child: Text(t('该服务器已开启邮箱登录，请登录或注册'),
                                style: TextStyle(color: AppTheme.textPrimary)),
                          ),
                        ],
                      ),
                      const SizedBox(height: 16),
                      TextField(
                        controller: _emailController,
                        keyboardType: TextInputType.emailAddress,
                        decoration: InputDecoration(
                          labelText: t('邮箱地址'),
                          hintText: 'you@example.com',
                          prefixIcon: Icon(Icons.alternate_email),
                        ),
                      ),
                      const SizedBox(height: 12),
                      Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Expanded(
                            child: TextField(
                              controller: _captchaInputController,
                              decoration: InputDecoration(
                                labelText: t('图形验证码'),
                                isDense: true,
                              ),
                            ),
                          ),
                          const SizedBox(width: 8),
                          Column(
                            children: [
                              GestureDetector(
                                onTap: _refreshCaptcha,
                                child: Container(
                                  width: 110,
                                  height: 40,
                                  decoration: BoxDecoration(
                                    color: AppTheme.surfaceColor,
                                    borderRadius: BorderRadius.circular(6),
                                    border: Border.all(color: AppTheme.borderColor),
                                  ),
                                  child: Builder(builder: (_) {
                                    final s = _captchaImage ?? '';
                                    final idx = s.indexOf('base64,');
                                    final b64 = idx >= 0 ? s.substring(idx + 7) : s;
                                    if (b64.isEmpty) {
                                      return Center(
                                        child: Text(t('获取验证码'),
                                            style: TextStyle(
                                                fontSize: 12,
                                                color: AppTheme.textSecondary)),
                                      );
                                    }
                                    try {
                                      return Image.memory(base64Decode(b64),
                                          fit: BoxFit.contain);
                                    } catch (_) {
                                      return const SizedBox.shrink();
                                    }
                                  }),
                                ),
                              ),
                              TextButton(
                                onPressed: _refreshCaptcha,
                                child: Text(t('换一张'),
                                    style: const TextStyle(fontSize: 12)),
                              ),
                            ],
                          ),
                        ],
                      ),
                      const SizedBox(height: 8),
                      SizedBox(
                        width: double.infinity,
                        child: OutlinedButton.icon(
                          onPressed: _requestEmailCode,
                          icon: const Icon(Icons.send_outlined, size: 18),
                          label: Text(t('发送邮箱验证码')),
                        ),
                      ),
                      const SizedBox(height: 12),
                      TextField(
                        controller: _emailCodeController,
                        decoration: InputDecoration(
                          labelText: t('邮箱验证码'),
                          prefixIcon: Icon(Icons.password),
                        ),
                      ),
                      const SizedBox(height: 16),
                      SizedBox(
                        width: double.infinity,
                        child: ElevatedButton(
                          onPressed: _doEmailLogin,
                          child: Text(t('登录并连接')),
                        ),
                      ),
                      const SizedBox(height: 8),
                      Center(
                        child: TextButton(
                          onPressed: () => setState(() => _state = _ConnectState.idle),
                          child: Text(t('取消')),
                        ),
                      ),
                    ],
                  ),
                ),
              ],

              if (_state == _ConnectState.verifying) const CircularProgressIndicator(),

              // ===== 需求 1：原两个按钮 → 单个【刷新】===== 
              if (_isConnected) ...[
                const SizedBox(height: 24),
                SizedBox(
                  width: 200,
                  child: ElevatedButton.icon(
                    onPressed: _manualRefresh,
                    icon: const Icon(Icons.refresh),
                    label: Text(t('刷新')),
                  ),
                ),
                const SizedBox(height: 12),
                SizedBox(
                  width: 200,
                  child: OutlinedButton.icon(
                    onPressed: _disconnect,
                    icon: const Icon(Icons.logout, size: 18),
                    label: Text(t('退出服务器')),
                    style: OutlinedButton.styleFrom(
                      foregroundColor: AppTheme.errorColor,
                      side: BorderSide(color: AppTheme.errorColor),
                    ),
                  ),
                ),
                const SizedBox(height: 12),
                TextButton.icon(
                  onPressed: _generateLogs,
                  icon: const Icon(Icons.bug_report, size: 18),
                  label: Text(t('生成日志')),
                  style: TextButton.styleFrom(
                    foregroundColor: AppTheme.textSecondary,
                  ),
                ),
              ],

              if (_state == _ConnectState.error && _errorMsg.isNotEmpty) ...[
                const SizedBox(height: 12),
                Container(
                  padding: const EdgeInsets.all(12),
                  decoration: BoxDecoration(
                    color: AppTheme.errorColor.withOpacity(0.1),
                    borderRadius: BorderRadius.circular(8),
                  ),
                  child: Text(
                    _errorMsg,
                    style: TextStyle(fontSize: 12, color: AppTheme.errorColor),
                    textAlign: TextAlign.center,
                  ),
                ),
                const SizedBox(height: 12),
                TextButton(
                  onPressed: () => setState(() => _state = _ConnectState.idle),
                  child: Text(t('重试')),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }

  Color get _stateColor {
    switch (_state) {
      case _ConnectState.idle:
        return AppTheme.textSecondary;
      case _ConnectState.applying:
        return AppTheme.warningColor;
      case _ConnectState.waitingApproval:
        return AppTheme.warningColor;
      case _ConnectState.verifying:
        return const Color(0xFF8B5CF6);
      case _ConnectState.connected:
        return AppTheme.successColor;
      case _ConnectState.emailLogin:
        return AppTheme.primaryColor;
      case _ConnectState.error:
        return AppTheme.errorColor;
    }
  }

  IconData get _stateIcon {
    switch (_state) {
      case _ConnectState.idle:
        return Icons.cloud_off_outlined;
      case _ConnectState.applying:
        return Icons.sync;
      case _ConnectState.waitingApproval:
        return Icons.hourglass_top;
      case _ConnectState.verifying:
        return Icons.verified;
      case _ConnectState.connected:
        return Icons.cloud_done;
      case _ConnectState.emailLogin:
        return Icons.mark_email_read_outlined;
      case _ConnectState.error:
        return Icons.error_outline;
    }
  }

  String get _stateText {
    switch (_state) {
      case _ConnectState.idle:
        return t('未连接');
      case _ConnectState.applying:
        return t('正在申请...');
      case _ConnectState.waitingApproval:
        return t('等待管理员审批');
      case _ConnectState.verifying:
        return t('正在验证...');
      case _ConnectState.connected:
        return t('已连接');
      case _ConnectState.emailLogin:
        return t('邮箱登录');
      case _ConnectState.error:
        return t('连接失败');
    }
  }
}
