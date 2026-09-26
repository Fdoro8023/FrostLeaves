import 'dart:io';

enum AppMode { server, client }

class PlatformHelper {
  static bool get isWindows => Platform.isWindows;
  static bool get isAndroid => Platform.isAndroid;
  static bool get isLinux => Platform.isLinux;
  static bool get isIOS => Platform.isIOS;

  /// Whether peer-to-peer mesh networking is supported
  static bool get supportsMesh => isWindows || isAndroid;

  /// Whether server mode is available
  static bool get supportsServerMode => isWindows;

  /// Available modes for current platform
  static List<AppMode> get availableModes {
    if (isWindows) return [AppMode.server, AppMode.client];
    return [AppMode.client];
  }

  /// Whether this is an HTTP-only client platform
  static bool get isHttpOnlyClient => isLinux || isIOS;
}
