import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../services/local_storage_service.dart';

/// 主题模式（默认深色）：深色 / 浅色 / 跟随系统
final themeModeProvider = StateProvider<ThemeMode>((ref) => ThemeMode.dark);

/// 读取已保存的主题模式（默认深色）
Future<ThemeMode> readSavedThemeMode() async {
  final s = await localStorageService.getThemeMode();
  switch (s) {
    case 'light':
      return ThemeMode.light;
    case 'system':
      return ThemeMode.system;
    default:
      return ThemeMode.dark;
  }
}

/// 保存主题模式
Future<void> persistThemeMode(ThemeMode mode) async {
  final s = mode == ThemeMode.light
      ? 'light'
      : (mode == ThemeMode.system ? 'system' : 'dark');
  await localStorageService.saveThemeMode(s);
}
