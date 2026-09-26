import 'dart:convert';
import 'dart:io';

import 'package:shared_preferences/shared_preferences.dart';

/// Local storage service for caching configuration
class LocalStorageService {
  static const String _keyServerUrl = 'server_url';
  static const String _keyDeviceId = 'device_id';
  static const String _keyToken = 'token';
  static const String _keyDeviceName = 'device_name';
  static const String _keyLastConnected = 'last_connected';

  Future<void> saveServerUrl(String url) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyServerUrl, url);
  }

  Future<String?> getServerUrl() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getString(_keyServerUrl);
  }

  Future<void> saveCredentials(String deviceId, String token) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyDeviceId, deviceId);
    await prefs.setString(_keyToken, token);
    await prefs.setString(_keyLastConnected, DateTime.now().toIso8601String());
  }

  Future<Map<String, String>?> getCredentials() async {
    final prefs = await SharedPreferences.getInstance();
    final deviceId = prefs.getString(_keyDeviceId);
    final token = prefs.getString(_keyToken);
    
    if (deviceId != null && token != null) {
      return {
        'deviceId': deviceId,
        'token': token,
        'lastConnected': prefs.getString(_keyLastConnected) ?? '',
      };
    }
    return null;
  }

  Future<void> saveDeviceName(String name) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyDeviceName, name);
  }

  Future<String?> getDeviceName() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getString(_keyDeviceName);
  }

  // ===== 用户协议（任务2） =====
  // 协议版本：条款有变更时 +1，可让所有人重新确认
  static const int agreementVersion = 2;
  static const String _keyAgreementAccepted = 'agreement_accepted';

  /// 安装位置指纹：同一份 exe 放在哪个目录，就属于哪个安装
  String _installTag() {
    try {
      final dir = File(Platform.resolvedExecutable).parent.path;
      var h = 0x811c9dc5;
      for (final c in utf8.encode(dir)) {
        h ^= c;
        h = (h * 0x01000193) & 0xFFFFFFFF;
      }
      return h.toRadixString(16).padLeft(8, '0');
    } catch (_) {
      return 'default';
    }
  }

  /// 安装目录里的同意标记文件（便携分发：换目录 = 全新安装 → 重新弹窗）
  File? _markerFile() {
    if (!Platform.isWindows) return null;
    try {
      final dir = File(Platform.resolvedExecutable).parent.path;
      return File(
          '$dir${Platform.pathSeparator}data${Platform.pathSeparator}agreement.json');
    } catch (_) {
      return null;
    }
  }

  Future<bool> isAgreementAccepted() async {
    final mf = _markerFile();
    if (mf != null) {
      // ① 首选：本安装目录的标记文件，且协议版本一致
      try {
        if (await mf.exists()) {
          final m = jsonDecode(await mf.readAsString());
          if (m is Map && (m['version'] as num?)?.toInt() == agreementVersion) {
            return true;
          }
        }
      } catch (_) {}
      // ② 兜底：带安装指纹的键（安装目录不可写时）
      try {
        final prefs = await SharedPreferences.getInstance();
        return prefs.getBool(
                '${_keyAgreementAccepted}_${_installTag()}_v$agreementVersion') ??
            false;
      } catch (_) {
        return false;
      }
    }
    // 非 Windows（如 Android）：应用数据本身即安装级
    final prefs = await SharedPreferences.getInstance();
    return prefs.getBool(
            '${_keyAgreementAccepted}_v$agreementVersion') ??
        false;
  }

  Future<void> acceptAgreement() async {
    final mf = _markerFile();
    if (mf != null) {
      try {
        await mf.parent.create(recursive: true);
        await mf.writeAsString(jsonEncode({
          'version': agreementVersion,
          'accepted_at': DateTime.now().toIso8601String(),
        }));
      } catch (_) {}
      try {
        final prefs = await SharedPreferences.getInstance();
        await prefs.setBool(
            '${_keyAgreementAccepted}_${_installTag()}_v$agreementVersion', true);
      } catch (_) {}
      return;
    }
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(
        '${_keyAgreementAccepted}_v$agreementVersion', true);
  }

  // ===== 语言（默认中文）=====
  static const String _keyLanguage = 'language';

  Future<String?> getLanguage() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getString(_keyLanguage);
  }

  Future<void> saveLanguage(String lang) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyLanguage, lang);
  }

  // ===== 外观主题（默认深色）=====
  static const String _keyThemeMode = 'theme_mode';

  Future<String?> getThemeMode() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getString(_keyThemeMode);
  }

  Future<void> saveThemeMode(String mode) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyThemeMode, mode);
  }

  Future<void> clearAll() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.clear();
  }
}

final localStorageService = LocalStorageService();
