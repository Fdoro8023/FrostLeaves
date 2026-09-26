import 'dart:convert';
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
  static const String _keyAgreementAccepted = 'agreement_accepted_v1';

  Future<bool> isAgreementAccepted() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getBool(_keyAgreementAccepted) ?? false;
  }

  Future<void> acceptAgreement() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_keyAgreementAccepted, true);
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
