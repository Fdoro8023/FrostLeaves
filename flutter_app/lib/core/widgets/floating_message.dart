import 'package:flutter/material.dart';
import '../../core/i18n/i18n.dart';
import '../theme/app_theme.dart';

/// A floating error/success message widget that appears in the bottom-right corner
class FloatingMessage extends StatefulWidget {
  final String title;
  final String message;
  final String? rawError;
  final MessageType type;
  final Duration duration;

  const FloatingMessage({
    super.key,
    required this.title,
    required this.message,
    this.rawError,
    this.type = MessageType.error,
    this.duration = const Duration(seconds: 5),
  });

  static void show({
    required BuildContext context,
    required String title,
    required String message,
    String? rawError,
    MessageType type = MessageType.error,
    Duration duration = const Duration(seconds: 5),
  }) {
    final overlay = Overlay.of(context);
    late OverlayEntry entry;
    
    entry = OverlayEntry(
      builder: (context) => Positioned(
        right: 16,
        bottom: 16,
        child: FloatingMessage(
          title: title,
          message: message,
          rawError: rawError,
          type: type,
          duration: duration,
        ),
      ),
    );
    
    overlay.insert(entry);
    Future.delayed(duration, () {
      entry.remove();
    });
  }

  @override
  State<FloatingMessage> createState() => _FloatingMessageState();
}

class _FloatingMessageState extends State<FloatingMessage> with SingleTickerProviderStateMixin {
  late AnimationController _controller;
  late Animation<double> _fadeAnimation;
  bool _expanded = false;

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 300),
    );
    _fadeAnimation = CurvedAnimation(
      parent: _controller,
      curve: Curves.easeInOut,
    );
    _controller.forward();
    
    // Auto dismiss
    Future.delayed(widget.duration - const Duration(milliseconds: 300), () {
      if (mounted) _controller.reverse();
    });
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  Color get _bgColor {
    switch (widget.type) {
      case MessageType.error: return const Color(0xFFDC2626);
      case MessageType.success: return AppTheme.successColor;
      case MessageType.warning: return AppTheme.warningColor;
      case MessageType.info: return AppTheme.primaryColor;
    }
  }

  IconData get _icon {
    switch (widget.type) {
      case MessageType.error: return Icons.error_outline;
      case MessageType.success: return Icons.check_circle_outline;
      case MessageType.warning: return Icons.warning_amber_outlined;
      case MessageType.info: return Icons.info_outline;
    }
  }

  String get _parsedMessage {
    final msg = widget.message;
    
    // Parse DioException
    if (msg.contains('DioException')) {
      if (msg.contains('404')) {
        return t('请求的资源未找到 (404)\n请检查服务端地址和端口是否正确');
      }
      if (msg.contains('401')) {
        return t('认证失败 (401)\n请重新登录或检查设备凭证');
      }
      if (msg.contains('403')) {
        return t('权限不足 (403)\n当前设备无权执行此操作');
      }
      if (msg.contains('connection error') || msg.contains('SocketException')) {
        return t('网络连接失败\n请检查服务端是否运行及网络是否通畅');
      }
      if (msg.contains('timeout')) {
        return t('请求超时\n请检查网络连接或稍后重试');
      }
    }
    
    // Parse HttpException
    if (msg.contains('HttpException')) {
      if (msg.contains('Connection closed')) {
        return t('连接被服务端关闭\n请检查服务端是否正常运行');
      }
    }
    
    return msg;
  }

  @override
  Widget build(BuildContext context) {
    return FadeTransition(
      opacity: _fadeAnimation,
      child: Material(
        color: Colors.transparent,
        child: Container(
          width: 360,
          constraints: const BoxConstraints(maxHeight: 400),
          decoration: BoxDecoration(
            color: AppTheme.cardColor,
            borderRadius: BorderRadius.circular(12),
            border: Border.all(color: _bgColor.withOpacity(0.3)),
            boxShadow: [
              BoxShadow(
                color: Colors.black.withOpacity(0.3),
                blurRadius: 12,
                offset: const Offset(0, 4),
              ),
            ],
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              // Header
              Container(
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: _bgColor.withOpacity(0.1),
                  borderRadius: const BorderRadius.only(
                    topLeft: Radius.circular(12),
                    topRight: Radius.circular(12),
                  ),
                ),
                child: Row(
                  children: [
                    Icon(_icon, color: _bgColor, size: 20),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        widget.title,
                        style: TextStyle(
                          fontSize: 14,
                          fontWeight: FontWeight.w600,
                          color: _bgColor,
                        ),
                      ),
                    ),
                    if (widget.rawError != null)
                      IconButton(
                        icon: Icon(
                          _expanded ? Icons.expand_less : Icons.expand_more,
                          size: 18,
                          color: AppTheme.textSecondary,
                        ),
                        padding: EdgeInsets.zero,
                        constraints: const BoxConstraints(),
                        onPressed: () {
                          setState(() => _expanded = !_expanded);
                        },
                      ),
                    IconButton(
                      icon: const Icon(Icons.close, size: 18),
                      padding: EdgeInsets.zero,
                      constraints: const BoxConstraints(),
                      onPressed: () {
                        _controller.reverse();
                      },
                    ),
                  ],
                ),
              ),
              // Message
              Padding(
                padding: const EdgeInsets.all(12),
                child: Text(
                  _parsedMessage,
                  style: TextStyle(
                    fontSize: 13,
                    color: AppTheme.textPrimary,
                    height: 1.4,
                  ),
                ),
              ),
              // Raw error (expandable)
              if (_expanded && widget.rawError != null)
                Container(
                  width: double.infinity,
                  padding: const EdgeInsets.all(12),
                  margin: const EdgeInsets.only(bottom: 8),
                  decoration: BoxDecoration(
                    color: Colors.black.withOpacity(0.3),
                    borderRadius: BorderRadius.circular(8),
                  ),
                  child: SingleChildScrollView(
                    child: SelectableText(
                      widget.rawError!,
                      style: TextStyle(
                        fontSize: 11,
                        color: AppTheme.textSecondary,
                        fontFamily: 'monospace',
                      ),
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

enum MessageType {
  error,
  success,
  warning,
  info,
}
