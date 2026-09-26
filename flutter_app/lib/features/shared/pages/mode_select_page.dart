import 'dart:io';
import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:video_player/video_player.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/utils/platform_helper.dart';
import '../../../core/services/local_storage_service.dart';

class ModeSelectPage extends ConsumerStatefulWidget {
  const ModeSelectPage({super.key});

  @override
  ConsumerState<ModeSelectPage> createState() => _ModeSelectPageState();
}

class _ModeSelectPageState extends ConsumerState<ModeSelectPage> {
  VideoPlayerController? _videoController;
  bool _videoInitialized = false;
  bool _showOptions = false;
  bool _hasError = false;

  @override
  void initState() {
    super.initState();
    _checkFirstLaunch();
    // 任务2①：首启协议闸门（本页 context 位于 MaterialApp 之下，可正常弹窗）
    WidgetsBinding.instance.addPostFrameCallback((_) => _ensureAgreement());
  }

  bool _agreementChecked = false;

  /// 首启协议摘要语言（false=中文，true=English）
  bool _agreementEn = false;

  Future<void> _ensureAgreement() async {
    if (_agreementChecked) return;
    _agreementChecked = true;
    if (await localStorageService.isAgreementAccepted()) return;
    if (!mounted) return;
    bool ok = false;
    debugPrint('[Agreement] showing dialog');
    final agreed = await showDialog<bool>(
      context: context,
      barrierDismissible: false,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx2, setSt) => PopScope(
          canPop: false,
          child: AlertDialog(
            title: Text(t('FrostLeaves 用户许可协议（摘要）')),
            content: SizedBox(
              width: 520,
              child: SingleChildScrollView(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        ChoiceChip(
                          label: const Text('中文'),
                          selected: !_agreementEn,
                          onSelected: (_) => setSt(() => _agreementEn = false),
                        ),
                        const SizedBox(width: 8),
                        ChoiceChip(
                          label: const Text('English'),
                          selected: _agreementEn,
                          onSelected: (_) => setSt(() => _agreementEn = true),
                        ),
                      ],
                    ),
                    const SizedBox(height: 12),
                    Text(
                      _agreementEn
                          ? 'By using this software you agree to the following terms:\n\n​\n\n1. FrostLeaves is a private file storage tool for personal non-commercial use only. All files are saved locally on your computer. The author will not collect your files. Please back up important data.\n\n​\n\n2. It supports peer-to-peer private networking and public address generation, which only calls a locally installed third-party networking client. The networking channel can only be used for remote access to private files between your own devices. Accessing overseas networks via this channel is strictly forbidden, and you shall bear all legal consequences. File encryption is implemented by built-in HTTPS and does not depend on the networking channel.\n\n​\n\n3. Do not store or spread illegal content, or use this software for cyberattacks.\n\n​\n\n4. The software is provided "as-is". The author is not responsible for file loss caused by hardware faults, attacks or misoperation.\n\n​\n\n5. Self-signed HTTPS certificates may show browser insecure warnings; the traffic is still encrypted.'
                          :
                      t('使用本软件即代表您同意：\n\n'
                        '1. FrostLeaves 为私有文件存储工具，以开源许可发布，禁止商业使用。所有文件保存在您本地电脑，作者不会收集您的文件。请自行备份重要数据。\n\n'
                        '2. 内置点对点私有组网、公网地址生成功能，仅调用本地已安装的第三方组网客户端。组网通道仅可用于自有设备之间远程访问私有文件，严禁用于访问境外网络，相关违法后果由使用者自行承担。文件传输加密由软件内置 HTTPS 独立实现，不依赖组网通道。\n\n'
                        '3. 不得使用本软件存储、传播违法违规内容，不得用于网络攻击等违法行为。\n\n'
                        '4. 软件按现状提供，不承诺绝对无故障，因硬件、配置、攻击、误操作带来文件损失，由用户自行承担。\n\n'
                        '5. 自签名 HTTPS 证书浏览器会提示不安全，属于正常现象，传输流量已加密。'),
                      style: TextStyle(fontSize: 13, height: 1.6),
                    ),
                    const SizedBox(height: 12),
                    Row(
                      children: [
                        Checkbox(value: ok, onChanged: (v) => setSt(() => ok = v ?? false)),
                        Expanded(child: Text(_agreementEn ? 'I have read and agree to this agreement' : t('我已阅读并同意协议'))),
                      ],
                    ),
                  ],
                ),
              ),
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(ctx2, false),
                child: Text(t('拒绝')),
              ),
              ElevatedButton(
                onPressed: ok ? () => Navigator.pop(ctx2, true) : null,
                child: Text(t('同意')),
              ),
            ],
          ),
        ),
      ),
    );
    if (agreed == true) {
      await localStorageService.acceptAgreement();
      debugPrint('[Agreement] accepted');
    } else {
      debugPrint('[Agreement] rejected, exiting');
      exit(0);
    }
  }

  Future<void> _checkFirstLaunch() async {
    final prefs = await SharedPreferences.getInstance();
    final hasSeenSplash = prefs.getBool('has_seen_splash') ?? false;
    
    if (hasSeenSplash) {
      // Skip video, show options immediately
      setState(() {
        _showOptions = true;
      });
    } else {
      // Show video
      _initVideo();
    }
  }

  Future<void> _initVideo() async {
    try {
      _videoController = VideoPlayerController.asset('assets/splash_video.mp4')
        ..initialize().then((_) {
          setState(() {
            _videoInitialized = true;
          });
          // Mute the video
          _videoController!.setVolume(0);
          _videoController!.play();
          
          // Listen for video completion
          _videoController!.addListener(() {
            if (_videoController!.value.position >= _videoController!.value.duration) {
              if (!_showOptions) {
                setState(() {
                  _showOptions = true;
                });
                // Mark as seen
                _markSplashSeen();
              }
            }
          });
        }).catchError((e) {
          setState(() {
            _hasError = true;
            _showOptions = true;
          });
        });
    } catch (e) {
      setState(() {
        _hasError = true;
        _showOptions = true;
      });
    }
  }

  Future<void> _markSplashSeen() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool('has_seen_splash', true);
  }

  @override
  void dispose() {
    _videoController?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final modes = PlatformHelper.availableModes;

    return Scaffold(
      body: Stack(
        children: [
          // Video background
          if (_videoInitialized && !_hasError)
            Positioned.fill(
              child: ClipRect(
                child: FittedBox(
                  fit: BoxFit.cover,
                  child: SizedBox(
                    width: _videoController!.value.size.width,
                    height: _videoController!.value.size.height,
                    child: VideoPlayer(_videoController!),
                  ),
                ),
              ),
            ),
          
          // Dark overlay
          Positioned.fill(
            child: Container(
              color: Colors.black.withOpacity(_showOptions ? 0.7 : 0.3),
            ),
          ),

          // Content
          Center(
            child: AnimatedOpacity(
              opacity: _showOptions ? 1.0 : 0.0,
              duration: const Duration(milliseconds: 800),
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  // Logo / Title
                  Container(
                    width: 80,
                    height: 80,
                    decoration: BoxDecoration(
                      color: AppTheme.primaryColor,
                      borderRadius: BorderRadius.circular(20),
                    ),
                    child: const Icon(Icons.cloud_outlined, size: 48, color: Colors.white),
                  ),
                  const SizedBox(height: 24),
                  Text(
                    'Private NetDisk',
                    style: TextStyle(
                      fontSize: 28,
                      fontWeight: FontWeight.bold,
                      color: AppTheme.textPrimary,
                      letterSpacing: -1,
                    ),
                  ),
                  const SizedBox(height: 8),
                  Text(
                    t('私有组网网盘 · v1.0.1'),
                    style: TextStyle(
                      fontSize: 14,
                      color: AppTheme.textSecondary,
                    ),
                  ),
                  const SizedBox(height: 48),

                  // Mode buttons
                  if (modes.contains(AppMode.server)) ...[
                    _ModeButton(
                      icon: Icons.dns_outlined,
                      title: t('启动服务端模式'),
                      subtitle: t('管理设备、审核入网、配置存储'),
                      color: AppTheme.primaryColor,
                      onTap: () => context.go('/server/dashboard'),
                    ),
                    const SizedBox(height: 16),
                  ],

                  _ModeButton(
                    icon: Icons.cloud_circle_outlined,
                    title: t('启动用户端模式'),
                    subtitle: t('连接网络、浏览文件、上传下载'),
                    color: const Color(0xFF8B5CF6),
                    onTap: () => context.go('/client/connect'),
                  ),

                  const SizedBox(height: 32),
                  Text(
                    t('当前平台: ${Platform.operatingSystem}'),
                    style: TextStyle(color: AppTheme.textSecondary, fontSize: 12),
                  ),
                ],
              ),
            ),
          ),

          // Skip button (top-right)
          if (_videoInitialized && !_showOptions)
            Positioned(
              top: 40,
              right: 20,
              child: TextButton(
                onPressed: () {
                  setState(() {
                    _showOptions = true;
                  });
                  _markSplashSeen();
                },
                child: Text(
                  t('跳过'),
                  style: TextStyle(color: Colors.white70),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _ModeButton extends StatelessWidget {
  final IconData icon;
  final String title;
  final String subtitle;
  final Color color;
  final VoidCallback onTap;

  const _ModeButton({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.color,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: 320,
      child: Material(
        color: AppTheme.cardColor,
        borderRadius: BorderRadius.circular(16),
        child: InkWell(
          borderRadius: BorderRadius.circular(16),
          onTap: onTap,
          child: Container(
            padding: const EdgeInsets.all(20),
            decoration: BoxDecoration(
              borderRadius: BorderRadius.circular(16),
              border: Border.all(color: AppTheme.borderColor),
            ),
            child: Row(
              children: [
                Container(
                  width: 52,
                  height: 52,
                  decoration: BoxDecoration(
                    color: color.withOpacity(0.15),
                    borderRadius: BorderRadius.circular(14),
                  ),
                  child: Icon(icon, color: color, size: 28),
                ),
                const SizedBox(width: 16),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        title,
                        style: TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w600,
                          color: AppTheme.textPrimary,
                        ),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        subtitle,
                        style: TextStyle(
                          fontSize: 12,
                          color: AppTheme.textSecondary,
                        ),
                      ),
                    ],
                  ),
                ),
                Icon(Icons.chevron_right, color: AppTheme.textSecondary),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
