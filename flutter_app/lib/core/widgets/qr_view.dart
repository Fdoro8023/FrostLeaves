import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../../core/i18n/i18n.dart';
import '../services/api_service.dart';

/// 分享二维码：二维码由服务端生成，管理接口需要管理令牌（且服务端模式需走回环），
/// 因此统一经 ApiService 取字节后用 Image.memory 渲染。
class QrView extends StatefulWidget {
  const QrView({
    super.key,
    required this.api,
    required this.text,
    this.size = 200,
  });

  final ApiService api;
  final String text;
  final double size;

  @override
  State<QrView> createState() => _QrViewState();
}

class _QrViewState extends State<QrView> {
  Future<List<int>>? _future;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void didUpdateWidget(covariant QrView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.text != widget.text) _load();
  }

  void _load() {
    if (widget.text.isEmpty) {
      _future = null;
      return;
    }
    _future = widget.api.fetchQrPng(widget.text);
  }

  @override
  Widget build(BuildContext context) {
    final f = _future;
    if (f == null) return SizedBox(width: widget.size, height: widget.size);
    return SizedBox(
      width: widget.size,
      height: widget.size,
      child: FutureBuilder<List<int>>(
        future: f,
        builder: (c, snap) {
          if (snap.connectionState != ConnectionState.done) {
            return const Center(
              child: SizedBox(
                  width: 26, height: 26, child: CircularProgressIndicator(strokeWidth: 2)),
            );
          }
          final bytes = snap.data;
          if (snap.hasError || bytes == null || bytes.isEmpty) {
            return Center(
              child: Text(t('二维码加载失败（请确认服务端已更新）'), textAlign: TextAlign.center),
            );
          }
          return Image.memory(Uint8List.fromList(bytes),
              width: widget.size, height: widget.size, gaplessPlayback: true);
        },
      ),
    );
  }
}