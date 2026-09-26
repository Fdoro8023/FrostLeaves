import '../../core/i18n/i18n.dart';
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:dio/io.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'local_storage_service.dart';

/// API service for communicating with the Frost Leaves server
/// Auth service runs on one port (e.g. 9091), file gateway on another (e.g. 9090)
class ApiService {
  late final Dio _authDio;
  late final Dio _fileDio;
  String _authUrl;
  String _fileUrl;
  String? _deviceId;
  String? _token;
  String? _adminToken;   // 服务端模式的管理通道令牌（需求 3）
  /// 服务端模式标记：管理接口必须走回环，否则会被服务端 403 拒绝
  bool serverMode = false;
  String? _deviceModel;  // 设备型号，随申请上报（需求 5）
  bool _credentialsLoaded = false;

  ApiService({String serverUrl = 'http://127.0.0.1:9091'})
      : _authUrl = serverUrl,
        _fileUrl = _deriveFileUrl(serverUrl) {

    _authDio = Dio(BaseOptions(
      baseUrl: _authUrl,
      connectTimeout: const Duration(seconds: 10),
      receiveTimeout: const Duration(seconds: 30),
      headers: {'Content-Type': 'application/json'},
    ));

    _fileDio = Dio(BaseOptions(
      baseUrl: _fileUrl,
      connectTimeout: const Duration(seconds: 10),
      receiveTimeout: const Duration(minutes: 5),
      headers: {'Content-Type': 'application/json'},
    ));

    // Add auth headers to both DIOs
    for (final dio in [_authDio, _fileDio]) {
      dio.interceptors.add(InterceptorsWrapper(
        onRequest: (options, handler) {
          // 服务端模式：管理请求强制回环（服务端规则：非回环未开远程管理 → 403）
          if (serverMode && isAdminPath(options.path)) {
            options.baseUrl = toLoopback(options.baseUrl);
          }
          if (_deviceId != null) {
            options.headers['X-Device-Id'] = _deviceId;
          }
          if (_token != null) {
            options.headers['X-Device-Token'] = _token;
          }
          if (_adminToken != null && _adminToken!.isNotEmpty) {
            options.headers['X-Admin-Token'] = _adminToken;
          }
          handler.next(options);
        },
        onError: (e, handler) async {
          // 兜底：管理接口因非回环被 403/401 时，自动改走 127.0.0.1 重试一次
          final code = e.response?.statusCode ?? 0;
          final path = e.requestOptions.path;
          if ((code == 403 || code == 401) &&
              isAdminPath(path) &&
              e.requestOptions.extra['loopbackRetried'] != true) {
            final loop = toLoopback(e.requestOptions.baseUrl);
            if (loop != e.requestOptions.baseUrl) {
              try {
                final opts = e.requestOptions
                  ..baseUrl = loop
                  ..extra['loopbackRetried'] = true;
                final resp = await dio.fetch(opts);
                return handler.resolve(resp);
              } catch (_) {
                // 继续走原错误
              }
            }
          }
          handler.next(e);
        },
      ));
    }
  }

  /// 管理接口前缀（这些接口在服务端模式必须走 127.0.0.1）
  static const List<String> _adminPrefixes = [
    '/api/v1/device/',
    '/api/v1/system/',
    '/api/v1/share',
    '/api/v1/tunnel',
    '/api/v1/admin/',
    '/api/v1/audit',
    '/api/v1/quota',
    '/api/v1/storage',
    '/api/v1/files/',
    '/api/v1/recycle/',
    '/api/v1/qr',
    '/api/v1/tls/',
  ];

  static bool isAdminPath(String path) =>
      _adminPrefixes.any((p) => path.startsWith(p));

  /// 把地址主机替换为 127.0.0.1（保留协议与端口）
  static String toLoopback(String url) {
    try {
      final u = Uri.parse(url);
      if (u.host == '127.0.0.1' || u.host == 'localhost' || u.host == '::1') return url;
      if (u.host.isEmpty) return url;
      // 服务端设计：局域网走 HTTPS，回环保留明文 HTTP（用 TLS 打回环会 WRONG_VERSION_NUMBER）
      return 'http://127.0.0.1:${u.port}';
    } catch (_) {
      return url;
    }
  }

  /// Derive file gateway URL from auth URL
  /// e.g. http://192.168.1.35:9091 -> http://192.168.1.35:9090
  static String _deriveFileUrl(String authUrl) {
    final uri = Uri.parse(authUrl);
    final filePort = (uri.port - 1).clamp(1, 65535);
    return '${uri.scheme}://${uri.host}:$filePort';
  }

  Future<void> loadSavedCredentials() async {
    if (_credentialsLoaded) return;
    
    try {
      final creds = await localStorageService.getCredentials();
      if (creds != null) {
        _deviceId = creds['deviceId'];
        _token = creds['token'];
      }
      
      final savedUrl = await localStorageService.getServerUrl();
      if (savedUrl != null && savedUrl.isNotEmpty) {
        _authUrl = savedUrl;
        _fileUrl = _deriveFileUrl(savedUrl);
        _authDio.options.baseUrl = _authUrl;
        _fileDio.options.baseUrl = _fileUrl;
      }
      
      _credentialsLoaded = true;
    } catch (e) {
      print('Error loading credentials: $e');
    }
  }

  void setServerUrl(String url) {
    _authUrl = url;
    _fileUrl = _deriveFileUrl(url);
    _authDio.options.baseUrl = _authUrl;
    _fileDio.options.baseUrl = _fileUrl;
    localStorageService.saveServerUrl(url);
  }

  void setCredentials(String deviceId, String token) {
    _deviceId = deviceId;
    _token = token;
    localStorageService.saveCredentials(deviceId, token);
  }

  /// 需求 5：取设备型号（Android 走原生 MethodChannel，其他平台回退系统版本）
  Future<String> resolveDeviceModel() async {
    if (Platform.isAndroid) {
      try {
        const channel = MethodChannel('frostleaves/device');
        final v = await channel.invokeMethod<String>('getDeviceModel');
        if (v != null && v.isNotEmpty) return v;
      } catch (_) {
        // 原生通道不可用时回退
      }
    }
    final raw = Platform.operatingSystemVersion;
    if (Platform.isWindows) {
      // 注册表 ProductName 在 Win11 上仍写着 Windows 10，用构建号判定真实版本
      final m = RegExp(r'Build (\d+)').firstMatch(raw);
      final build = int.tryParse(m?.group(1) ?? '') ?? 0;
      final head = raw.split('(').first.trim(); // 例：Windows 10 Pro 10.0
      final noVer = head.replaceAll(RegExp(r'\s*\d+\.\d+$'), '').trim(); // Windows 10 Pro
      final edition = noVer.replaceFirst(RegExp(r'^Windows \d+'), '').trim(); // Pro
      final win = build >= 22000 ? 'Windows 11' : 'Windows 10';
      final suffix = build > 0 ? ' (Build $build)' : '';
      return ('$win $edition').trim() + suffix;
    }
    return raw;
  }

  /// 管理通道令牌（服务端模式使用；由 /api/v1/admin/token 获取）
  void setAdminToken(String? token) {
    _adminToken = token;
  }

  String? get adminToken => _adminToken;

  /// 设备型号（Android 端用于需求 5 的型号上报）
  void setDeviceModel(String? model) {
    _deviceModel = model;
  }

  void clearCredentials() {
    _deviceId = null;
    _token = null;
    localStorageService.clearAll();
    _credentialsLoaded = false;
  }

  // ========== Device APIs (auth service) ==========

  Future<Map<String, dynamic>> applyDevice(String deviceId, String deviceName, String platform) async {
    await loadSavedCredentials();
    final resp = await _authDio.post(
      '/api/v1/device/apply',
      data: {
        'device_id': deviceId,
        'device_name': deviceName,
        'platform': platform,
      },
      options: Options(headers: {
        if (_deviceModel != null && _deviceModel!.isNotEmpty) 'X-Device-Model': _deviceModel!,
      }),
    );
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> verifyCode(String deviceId, String code) async {
    await loadSavedCredentials();
    final resp = await _authDio.post('/api/v1/device/verify', data: {
      'device_id': deviceId,
      'code': code,
    });
    return resp.data as Map<String, dynamic>;
  }

  /// 设备自查状态（客户端轮询，需求 1）：返回 status / token_valid / server_name
  Future<Map<String, dynamic>> getDeviceStatus(String deviceId) async {
    final resp = await _authDio.get('/api/v1/device/status',
        queryParameters: {'device_id': deviceId});
    return resp.data as Map<String, dynamic>;
  }

  /// 客户端主动退出服务器（设备令牌鉴权）：服务端撤销令牌并移除设备记录
  Future<Map<String, dynamic>> releaseDevice() async {
    await loadSavedCredentials();
    final resp = await _authDio.post('/api/v1/device/release');
    return resp.data as Map<String, dynamic>;
  }

  /// 获取管理通道令牌（仅本机回环可用，需求 3）
  /// 二维码 PNG：走管理通道（服务端模式自动改回环 + 带 X-Admin-Token）
  Future<List<int>> fetchQrPng(String text, {int scale = 8}) async {
    final resp = await _authDio.get<List<int>>(
      '/api/v1/qr',
      queryParameters: {'text': text, 'scale': scale},
      options: Options(responseType: ResponseType.bytes),
    );
    return resp.data ?? <int>[];
  }

  Future<Map<String, dynamic>> fetchAdminToken() async {
    final resp = await _authDio.get('/api/v1/admin/token');
    return resp.data as Map<String, dynamic>;
  }

  // ===== HTTPS / 自签 CA 支持 =====
  SecurityContext? _securityContext;

  /// 是否已信任自签 CA
  bool get hasTrustedCa => _securityContext != null;

  /// 信任给定 PEM 的 CA，并让文件/授权两个 Dio 走它
  void trustCa(List<int> caPem) {
    final ctx = SecurityContext(withTrustedRoots: true);
    ctx.setTrustedCertificatesBytes(caPem);
    _securityContext = ctx;
    _authDio.httpClientAdapter = IOHttpClientAdapter(
      createHttpClient: () => HttpClient(context: ctx),
    );
    _fileDio.httpClientAdapter = IOHttpClientAdapter(
      createHttpClient: () => HttpClient(context: ctx),
    );
  }

  /// 从 CA 引导端口（明文 HTTP，仅公开证书）取回 CA 并信任
  Future<bool> bootstrapCa(String host, {int port = 9093}) async {
    try {
      final raw = Dio(BaseOptions(
        connectTimeout: const Duration(seconds: 8),
        receiveTimeout: const Duration(seconds: 8),
      ));
      final resp = await raw.get<String>(
        'http://$host:$port/ca.crt',
        options: Options(responseType: ResponseType.plain),
      );
      final pem = resp.data ?? '';
      if (!pem.contains('BEGIN CERTIFICATE')) return false;
      trustCa(pem.codeUnits);
      return true;
    } catch (_) {
      return false;
    }
  }

  /// 取回 CA 证书 PEM 字节（回环管理员通道）
  Future<List<int>> fetchCaPemBytes() async {
    await loadSavedCredentials();
    final resp = await _authDio.get('/api/v1/tls/ca.crt',
        options: Options(responseType: ResponseType.bytes));
    return (resp.data as List).cast<int>();
  }

  /// 系统指标（关于页）：CPU / 内存 / 磁盘 / 本程序占用
  Future<Map<String, dynamic>> getSystemMetrics() async {
    await loadSavedCredentials();
    final resp = await _authDio.get('/api/v1/system/metrics',
        options: Options(receiveTimeout: const Duration(seconds: 30)));
    return resp.data as Map<String, dynamic>;
  }

  /// 证书信息（指纹 / SAN / https 地址）
  Future<Map<String, dynamic>> getTlsInfo() async {
    await loadSavedCredentials();
    final resp = await _authDio.get('/api/v1/tls/info',
        options: Options(receiveTimeout: const Duration(seconds: 30)));
    return resp.data as Map<String, dynamic>;
  }

  /// 把 Dio 异常翻译成「可读原因」，包含服务端返回 body（便于自查 400/403/429）
  static String describeError(Object e) {
    if (e is DioException) {
      final status = e.response?.statusCode;
      final data = e.response?.data;
      String body = '';
      if (data is Map) {
        body = (data['error'] ?? data['msg'] ?? '').toString();
      } else if (data is String) {
        body = data;
      }
      if (body.isNotEmpty) {
        return status != null ? 'HTTP $status：$body' : body;
      }
      return 'HTTP ${status ?? '-'}：${e.message ?? e.type.name}';
    }
    return e.toString();
  }

  /// 网络信息（LAN 地址 / 端口 / 公网地址）——用于生成正确的分享链接
  Future<Map<String, dynamic>> getSystemNetwork() async {
    await loadSavedCredentials();
    final resp = await _authDio.get('/api/v1/system/network');
    return resp.data as Map<String, dynamic>;
  }

  /// 隧道环境探测（需求 7）
  Future<Map<String, dynamic>> detectTunnel() async {
    await loadSavedCredentials();
    final resp = await _authDio.get('/api/v1/tunnel/detect',
        options: Options(receiveTimeout: const Duration(minutes: 3)));
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> listDevices({String? status}) async {
    await loadSavedCredentials();
    final resp = await _authDio.get('/api/v1/device/list',
        queryParameters: status != null ? {'status': status} : null);
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> approveDevice(String deviceId) async {
    await loadSavedCredentials();
    final resp = await _authDio.post('/api/v1/device/$deviceId/approve');
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> rejectDevice(String deviceId) async {
    await loadSavedCredentials();
    final resp = await _authDio.post('/api/v1/device/$deviceId/reject');
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> blacklistDevice(String deviceId) async {
    await loadSavedCredentials();
    final resp = await _authDio.post('/api/v1/device/$deviceId/blacklist');
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> unblockDevice(String deviceId) async {
    await loadSavedCredentials();
    final resp = await _authDio.post('/api/v1/device/$deviceId/unblock');
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> disconnectDevice(String deviceId) async {
    await loadSavedCredentials();
    final resp = await _authDio.post('/api/v1/device/$deviceId/disconnect');
    return resp.data as Map<String, dynamic>;
  }

  // ========== File APIs (file gateway) ==========

  Future<Map<String, dynamic>> listFiles(String path) async {
    await loadSavedCredentials();
    final resp = await _fileDio.get('/api/v1/files/list',
        queryParameters: {'path': path});
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> uploadFile(String localPath, String remotePath,
      {void Function(int, int)? onProgress}) async {
    await loadSavedCredentials();
    final formData = FormData.fromMap({
      'file': await MultipartFile.fromFile(localPath),
      'path': remotePath,
    });
    final resp = await _fileDio.post('/api/v1/files/upload',
        data: formData,
        onSendProgress: onProgress);
    return resp.data as Map<String, dynamic>;
  }

  Future<Response> downloadFile(String remotePath, {void Function(int, int)? onProgress}) async {
    await loadSavedCredentials();
    final resp = await _fileDio.get(
      '/api/v1/files/download',
      queryParameters: {'path': remotePath},
      options: Options(responseType: ResponseType.bytes),
      onReceiveProgress: onProgress,
    );
    return resp;
  }

  Future<Map<String, dynamic>> deleteFiles(List<String> paths) async {
    await loadSavedCredentials();
    final resp = await _fileDio.delete('/api/v1/files/delete', data: {'paths': paths});
    return resp.data as Map<String, dynamic>;
  }

  // ========== Recycle Bin API ==========

  Future<Map<String, dynamic>> getRecycleList() async {
    await loadSavedCredentials();
    final resp = await _fileDio.get('/api/v1/recycle/list');
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> restoreFromRecycle(String id) async {
    await loadSavedCredentials();
    final resp = await _fileDio.post('/api/v1/recycle/restore', data: {'id': id});
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> deleteFromRecycle(List<String> ids) async {
    await loadSavedCredentials();
    final resp = await _fileDio.delete('/api/v1/recycle/delete', data: {'ids': ids});
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> clearRecycle() async {
    await loadSavedCredentials();
    final resp = await _fileDio.delete('/api/v1/recycle/clear');
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> renameFile(String oldPath, String newPath) async {
    await loadSavedCredentials();
    final resp = await _fileDio.post('/api/v1/files/rename', data: {
      'old_path': oldPath,
      'new_path': newPath,
    });
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> createDirectory(String path) async {
    await loadSavedCredentials();
    final resp = await _fileDio.post('/api/v1/files/mkdir', data: {
      'path': path,
    });
    return resp.data as Map<String, dynamic>;
  }

  // ========== Quota APIs ==========

  Future<Map<String, dynamic>> getQuotaList() async {
    await loadSavedCredentials();
    final resp = await _authDio.get('/api/v1/quota/list');
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> setQuota(Map<String, dynamic> config) async {
    await loadSavedCredentials();
    final resp = await _authDio.post('/api/v1/quota', data: config);
    return resp.data as Map<String, dynamic>;
  }

  // ========== Audit APIs ==========

  Future<Map<String, dynamic>> getAuditLogs({String? deviceId, String? action}) async {
    await loadSavedCredentials();
    final resp = await _authDio.get('/api/v1/audit/logs',
        queryParameters: {
          if (deviceId != null) 'device_id': deviceId,
          if (action != null) 'action': action,
        });
    return resp.data as Map<String, dynamic>;
  }

  // ========== System APIs ==========

  Future<Map<String, dynamic>> getSystemStatus() async {
    await loadSavedCredentials();
    final resp = await _authDio.get('/api/v1/system/status');
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> getSystemConfig() async {
    await loadSavedCredentials();
    final resp = await _authDio.get('/api/v1/system/config');
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> updateSystemConfig(Map<String, dynamic> config) async {
    await loadSavedCredentials();
    final resp = await _authDio.post('/api/v1/system/config', data: config);
    return resp.data as Map<String, dynamic>;
  }

  // ========== Share APIs (admin) ==========

  Future<Map<String, dynamic>> listShares() async {
    await loadSavedCredentials();
    final resp = await _authDio.get('/api/v1/share/list');
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> createShare({
    required String target,
    String perm = 'read',
    int expiresHours = 24,
    int maxDownloads = 0,
    String note = '',
    List<String>? files,
  }) async {
    await loadSavedCredentials();
    final resp = await _authDio.post('/api/v1/share/create', data: {
      'target': target,
      'perm': perm,
      'expires_hours': expiresHours,
      'max_downloads': maxDownloads,
      'note': note,
      if (files != null && files.isNotEmpty) 'files': files,
    });
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> revokeShare(String code) async {
    await loadSavedCredentials();
    final resp = await _authDio.post('/api/v1/share/revoke', data: {'code': code});
    return resp.data as Map<String, dynamic>;
  }

  // ========== Tunnel APIs (admin, mesh public access) ==========

  Future<Map<String, dynamic>> getTunnelStatus() async {
    await loadSavedCredentials();
    final resp = await _authDio.get('/api/v1/tunnel/status',
        options: Options(receiveTimeout: const Duration(minutes: 3)));
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> enableTunnel() async {
    await loadSavedCredentials();
    final resp = await _authDio.post('/api/v1/tunnel/enable',
        options: Options(receiveTimeout: const Duration(minutes: 3)));
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> disableTunnel() async {
    await loadSavedCredentials();
    final resp = await _authDio.post('/api/v1/tunnel/disable',
        options: Options(receiveTimeout: const Duration(minutes: 3)));
    return resp.data as Map<String, dynamic>;
  }

  // ========== Getters ==========

  String get authUrl => _authUrl;
  String get fileUrl => _fileUrl;
  String? get deviceId => _deviceId;
  String? get token => _token;
}

final apiServiceProvider = Provider<ApiService>((ref) {
  return ApiService();
});
