import 'dart:io';

import 'package:flutter/material.dart';
import 'package:open_file/open_file.dart';

import '../i18n/i18n.dart';
import 'api_service.dart';

/// 需求 2：检查更新的通用流程（客户端 / 服务端【关于】页共用）。
///
/// 流程：检查 → 有更新则询问 → 下载 → 安装。
/// 安装：Android 交给系统安装器；Windows 用辅助脚本在退出后替换并重启。
Future<void> runUpdateCheck(
  BuildContext context,
  ApiService api, {
  bool refresh = true,
}) async {
  // 1) 检查
  _showBlocking(context, t('正在检查更新…'));
  Map<String, dynamic> data;
  try {
    final resp = await api.checkUpdate(refresh: refresh);
    data = (resp['data'] as Map?)?.cast<String, dynamic>() ?? {};
  } catch (e) {
    if (context.mounted) Navigator.of(context, rootNavigator: true).pop();
    if (context.mounted) {
      await _info(context, t('检查更新失败'), ApiService.describeError(e));
    }
    return;
  }
  if (context.mounted) Navigator.of(context, rootNavigator: true).pop();

  final err = (data['error'] ?? '').toString();
  if (err.isNotEmpty) {
    if (context.mounted) {
      await _info(context, t('检查更新失败'), err);
    }
    return;
  }

  final current = (data['current_version'] ?? ApiService.clientVersion).toString();
  final latest = (data['latest_version'] ?? '').toString();
  final hasUpdate = data['has_update'] == true;
  final notes = (data['notes'] ?? '').toString();
  final assets = (data['assets'] as List?) ?? const [];

  if (!hasUpdate) {
    if (context.mounted) {
      await _info(context, t('已是最新版本'),
          '${t('当前版本')}: $current\n${t('最新版本')}: ${latest.isEmpty ? current : latest}');
    }
    return;
  }

  if (!context.mounted) return;
  final go = await showDialog<bool>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: Text(t('发现新版本')),
      content: SizedBox(
        width: 460,
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Text('${t('当前版本')}: $current'),
              const SizedBox(height: 4),
              Text('${t('最新版本')}: $latest',
                  style: const TextStyle(fontWeight: FontWeight.w600)),
              if (notes.trim().isNotEmpty) ...[
                const SizedBox(height: 12),
                Text(t('更新说明'), style: const TextStyle(fontWeight: FontWeight.w600)),
                const SizedBox(height: 4),
                Text(notes.trim(), style: const TextStyle(fontSize: 12, height: 1.5)),
              ],
            ],
          ),
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(ctx, false), child: Text(t('稍后'))),
        ElevatedButton(
          onPressed: () => Navigator.pop(ctx, true),
          child: Text(t('下载并安装')),
        ),
      ],
    ),
  );
  if (go != true) return;

  // 2) 选择适合当前平台的更新包
  final asset = _pickAsset(assets);
  if (asset == null) {
    if (context.mounted) {
      await _info(context, t('没有可用的更新包'), t('该版本未提供适配当前平台的安装包。'));
    }
    return;
  }

  if (!context.mounted) return;
  _showBlocking(context, t('正在下载更新包…'));
  String localPath;
  try {
    final resp = await api.downloadUpdate(asset['url'] as String, asset['name'] as String);
    localPath = (resp['data']?['path'] ?? '').toString();
  } catch (e) {
    if (context.mounted) Navigator.of(context, rootNavigator: true).pop();
    if (context.mounted) {
      await _info(context, t('下载失败'), ApiService.describeError(e));
    }
    return;
  }
  if (context.mounted) Navigator.of(context, rootNavigator: true).pop();

  // 3) 安装
  await _install(context, localPath, asset['name'] as String);
}

Map<String, dynamic>? _pickAsset(List assets) {
  final wanted = Platform.isAndroid
      ? const ['android', 'apk']
      : const ['windows', 'win'];
  Map<String, dynamic>? fallback;
  for (final a in assets) {
    final m = (a as Map).cast<String, dynamic>();
    final name = (m['name'] ?? '').toString().toLowerCase();
    if (name.endsWith('.apk')) fallback ??= m;
    for (final w in wanted) {
      if (name.contains(w)) return m;
    }
  }
  return fallback;
}

Future<void> _install(BuildContext context, String localPath, String assetName) async {
  if (localPath.isEmpty || !File(localPath).existsSync()) {
    if (context.mounted) {
      await _info(context, t('安装失败'), t('更新包不存在或已被移除。'));
    }
    return;
  }

  // Android：交给系统安装器
  if (Platform.isAndroid || localPath.toLowerCase().endsWith('.apk')) {
    final res = await OpenFile.open(localPath);
    if (context.mounted && res.type != ResultType.done) {
      await _info(context, t('已下载更新包'), '${t('更新包位置')}:\n$localPath\n\n${t('请手动点击安装。')}');
    }
    return;
  }

  // Windows：解压（若是 zip）后，用辅助脚本退出时替换并重启
  if (!Platform.isWindows) {
    if (context.mounted) {
      await _info(context, t('已下载更新包'), '${t('更新包位置')}:\n$localPath');
    }
    return;
  }

  String sourceDir;
  String exeName = '';
  try {
    if (localPath.toLowerCase().endsWith('.zip')) {
      final outDir = '${File(localPath).parent.path}\\${assetName.replaceAll(RegExp(r'[^A-Za-z0-9._-]'), '_')}_x';
      await Process.run('powershell', [
        '-NoProfile',
        '-Command',
        'Expand-Archive -LiteralPath "$localPath" -DestinationPath "$outDir" -Force',
      ]);
      sourceDir = outDir;
      exeName = _findExe(outDir);
    } else {
      sourceDir = File(localPath).parent.path;
      exeName = localPath.split(Platform.pathSeparator).last;
    }
  } catch (e) {
    if (context.mounted) {
      await _info(context, t('安装失败'), '$e\n\n${t('更新包位置')}:\n$localPath');
    }
    return;
  }

  if (exeName.isEmpty) {
    if (context.mounted) {
      await _info(context, t('已下载更新包'),
          '${t('更新包位置')}:\n$localPath\n\n${t('未在包内找到可执行程序，请手动替换。')}');
    }
    return;
  }

  if (!context.mounted) return;
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: Text(t('准备安装更新')),
      content: Text(
        '${t('将关闭当前程序、替换文件并自动重启。')}\n\n'
        '${t('更新包位置')}:\n$localPath',
        style: const TextStyle(height: 1.5),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(ctx, false), child: Text(t('取消'))),
        ElevatedButton(onPressed: () => Navigator.pop(ctx, true), child: Text(t('立即安装并重启'))),
      ],
    ),
  );
  if (ok != true) return;

  final targetDir = File(Platform.resolvedExecutable).parent.path;
  final batPath = '${Directory.systemTemp.path}\\fl_update_$pid.bat';
  final script = '''
@echo off
setlocal
:wait
tasklist /FI "PID eq $pid" 2>nul | find "$pid" >nul
if not errorlevel 1 (
  timeout /t 1 /nobreak >nul
  goto wait
)
xcopy /E /Y /I "$sourceDir\\*" "$targetDir\\" >nul
start "" "$targetDir\\$exeName"
del "%~f0"
''';
  await File(batPath).writeAsString(script);
  await Process.start('cmd.exe', ['/c', batPath], mode: ProcessStartMode.detached);
  exit(0);
}

String _findExe(String dir) {
  try {
    for (final f in Directory(dir).listSync(recursive: true)) {
      if (f is File) {
        final n = f.path.split(Platform.pathSeparator).last.toLowerCase();
        if (n == 'frostleaves_server.exe') return f.path.split(Platform.pathSeparator).last;
      }
    }
    for (final f in Directory(dir).listSync(recursive: true)) {
      if (f is File) {
        final n = f.path.split(Platform.pathSeparator).last.toLowerCase();
        if (n.endsWith('.exe')) return f.path.split(Platform.pathSeparator).last;
      }
    }
  } catch (_) {}
  return '';
}

void _showBlocking(BuildContext context, String message) {
  showDialog<void>(
    context: context,
    barrierDismissible: false,
    builder: (_) => AlertDialog(
      content: Row(
        children: [
          const SizedBox(width: 22, height: 22, child: CircularProgressIndicator(strokeWidth: 2.4)),
          const SizedBox(width: 16),
          Expanded(child: Text(message)),
        ],
      ),
    ),
  );
}

Future<void> _info(BuildContext context, String title, String body) {
  return showDialog<void>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: Text(title),
      content: SizedBox(
        width: 440,
        child: SingleChildScrollView(child: Text(body, style: const TextStyle(height: 1.5))),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(ctx), child: Text(t('知道了'))),
      ],
    ),
  );
}
