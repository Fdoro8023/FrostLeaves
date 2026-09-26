import 'package:flutter_riverpod/flutter_riverpod.dart';

/// 全局连接状态（响应式）：连接页负责更新，外壳/状态区 watch 它，
/// 保证被踢出或主动退出后所有界面同步显示【未连接】。
final connectionStateProvider = StateProvider<bool>((ref) => false);
