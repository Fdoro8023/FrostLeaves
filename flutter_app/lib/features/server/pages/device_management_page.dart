import 'dart:async';

import 'package:flutter/material.dart';
import '../../../core/i18n/i18n.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/services/api_service.dart';
import '../../../core/models/models.dart';

class DeviceManagementPage extends ConsumerStatefulWidget {
  const DeviceManagementPage({super.key});
  @override
  ConsumerState<DeviceManagementPage> createState() => _DeviceManagementPageState();
}

class _DeviceManagementPageState extends ConsumerState<DeviceManagementPage> with SingleTickerProviderStateMixin {
  late TabController _tabController;
  List<DeviceModel> _devices = [];
  Timer? _refreshTimer;  // 需求 4：实时刷新定时器
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 3, vsync: this);
    _loadDevices();
    // 需求 4：每 2 秒静默刷新（实时反映客户端退出/被踢）
    _refreshTimer = Timer.periodic(const Duration(seconds: 2), (_) {
      if (!mounted) return;
      _silentRefresh();
    });
    // 切换标签页时立即拉一次，避免看到旧状态
    _tabController.addListener(() {
      if (_tabController.indexIsChanging) return;
      if (mounted) _silentRefresh();
    });
  }

  @override
  void dispose() {
    _refreshTimer?.cancel();
    _tabController.dispose();
    super.dispose();
  }

  Future<void> _loadDevices() async {
    setState(() => _loading = true);
    try {
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();
      final resp = await api.listDevices();
      final data = resp['data'] as Map<String, dynamic>?;
      final items = (data?['items'] as List?)?.map((e) => DeviceModel.fromJson(e as Map<String, dynamic>)).toList() ?? [];
      setState(() { _devices = items; _loading = false; });
    } catch (e) {
      setState(() => _loading = false);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t(t('加载失败: $e')))),
        );
      }
    }
  }

  /// 需求 4：静默刷新（不显示 loading、不弹错误提示，避免干扰）
  Future<void> _silentRefresh() async {
    try {
      final api = ref.read(apiServiceProvider);
      final resp = await api.listDevices();
      final data = resp['data'] as Map<String, dynamic>?;
      final items = (data?['items'] as List?)
              ?.map((e) => DeviceModel.fromJson(e as Map<String, dynamic>))
              .toList() ??
          [];
      if (!mounted) return;
      setState(() { _devices = items; _loading = false; });
    } catch (_) {
      // 静默失败，等下一轮
    }
  }

  List<DeviceModel> get _pendingDevices => _devices.where((d) => d.status == 'pending').toList();
  List<DeviceModel> get _connectedDevices => _devices.where((d) => d.status == 'connected').toList();
  List<DeviceModel> get _blockedDevices => _devices.where((d) => d.status == 'blacklisted').toList();

  Future<void> _approveDevice(String deviceId) async {
    try {
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();
      final resp = await api.approveDevice(deviceId);
      final data = resp['data'] as Map<String, dynamic>?;
      final code = data?['code'] as String?;

      if (mounted && code != null) {
        showDialog(
          context: context,
          builder: (ctx) => AlertDialog(
            title: Text(t(t('验证码已生成'))),
            content: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(t(t('请将以下验证码发送给设备用户：'))),
                const SizedBox(height: 16),
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 12),
                  decoration: BoxDecoration(
                    color: AppTheme.surfaceColor,
                    borderRadius: BorderRadius.circular(8),
                  ),
                  child: SelectableText(
                    code,
                    style: TextStyle(
                      fontSize: 28,
                      fontWeight: FontWeight.bold,
                      letterSpacing: 4,
                      color: AppTheme.primaryColor,
                    ),
                  ),
                ),
                const SizedBox(height: 8),
                Text(t(t('有效期 5 分钟，一次性使用')), style: TextStyle(fontSize: 12, color: AppTheme.textSecondary)),
              ],
            ),
            actions: [
              TextButton(
                onPressed: () {
                  Clipboard.setData(ClipboardData(text: code));
                  Navigator.pop(ctx);
                },
                child: Text(t(t('复制并关闭'))),
              ),
            ],
          ),
        );
      }
      _loadDevices();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t(t('操作失败: $e')))),
        );
      }
    }
  }

  Future<void> _disconnectDevice(String deviceId, String deviceName) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t(t('确认踢出'))),
        content: Text(t(t('确定要踢出设备 "${deviceName.isEmpty ? deviceId : deviceName}" 吗？\n该设备需要重新验证才能连接。'))),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text(t(t('取消'))),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(t(t('踢出')), style: TextStyle(color: AppTheme.errorColor)),
          ),
        ],
      ),
    );

    if (confirmed != true) return;

    try {
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();
      await api.disconnectDevice(deviceId);
      _loadDevices();
      
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t(t('设备已踢出')))),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t(t('操作失败: $e')))),
        );
      }
    }
  }

  Future<void> _blacklistDevice(String deviceId, String deviceName) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t(t('确认拉黑'))),
        content: Text(t(t('确定要拉黑设备 "${deviceName.isEmpty ? deviceId : deviceName}" 吗？\n该设备将无法再连接到服务器。'))),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text(t(t('取消'))),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(t(t('拉黑')), style: TextStyle(color: AppTheme.errorColor)),
          ),
        ],
      ),
    );

    if (confirmed != true) return;

    try {
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();
      await api.blacklistDevice(deviceId);
      _loadDevices();
      
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t(t('设备已拉黑')))),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t(t('操作失败: $e')))),
        );
      }
    }
  }

  Future<void> _unblockDevice(String deviceId, String deviceName) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t(t('确认解除'))),
        content: Text(t(t('确定要解除设备 "${deviceName.isEmpty ? deviceId : deviceName}" 的黑名单吗？\n该设备将可以重新申请连接。'))),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text(t(t('取消'))),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(t(t('解除')), style: TextStyle(color: AppTheme.successColor)),
          ),
        ],
      ),
    );

    if (confirmed != true) return;

    try {
      final api = ref.read(apiServiceProvider);
      await api.loadSavedCredentials();
      await api.unblockDevice(deviceId);
      await _loadDevices();
      
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t(t('设备已解除黑名单')))),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(t(t('操作失败：$e')))),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(
                t(t('设备管理')),
                style: TextStyle(fontSize: 24, fontWeight: FontWeight.bold, color: AppTheme.textPrimary),
              ),
              const Spacer(),
              IconButton(
                icon: const Icon(Icons.refresh),
                onPressed: _loadDevices,
                tooltip: t(t('刷新')),
              ),
            ],
          ),
          const SizedBox(height: 16),
          TabBar(
            controller: _tabController,
            tabs: [
              Tab(text: t(t('待审核 (${_pendingDevices.length})'))),
              Tab(text: t(t('已连接 (${_connectedDevices.length})'))),
              Tab(text: t(t('已拒绝/拉黑 (${_blockedDevices.length})'))),
            ],
          ),
          const SizedBox(height: 16),
          Expanded(
            child: _loading
                ? const Center(child: CircularProgressIndicator())
                : TabBarView(
                    controller: _tabController,
                    children: [
                      _buildDeviceList(_pendingDevices, showApprovalActions: true),
                      _buildDeviceList(_connectedDevices, showConnectedActions: true),
                      _buildDeviceList(_blockedDevices, showBlockedActions: true),
                    ],
                  ),
          ),
        ],
      ),
    );
  }

  Widget _buildDeviceList(
    List<DeviceModel> devices, {
    bool showApprovalActions = false,
    bool showConnectedActions = false,
    bool showBlockedActions = false,
  }) {
    if (devices.isEmpty) {
      return Center(
        child: Text(t(t('暂无设备')), style: TextStyle(color: AppTheme.textSecondary)),
      );
    }
    
    return ListView.builder(
      itemCount: devices.length,
      itemBuilder: (context, index) {
        final d = devices[index];
        return Card(
          margin: const EdgeInsets.only(bottom: 8),
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Row(
              children: [
                Icon(_platformIcon(d.platform), color: AppTheme.primaryColor, size: 28),
                const SizedBox(width: 16),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        d.name.isEmpty ? d.id : d.name,
                        style: TextStyle(fontWeight: FontWeight.w600, color: AppTheme.textPrimary),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        'ID: ${d.id} · ${d.platform}${d.model.isNotEmpty ? ' · ${d.model}' : ''}',
                        style: TextStyle(fontSize: 12, color: AppTheme.textSecondary),
                      ),
                      if (d.ipAddress != null && d.ipAddress!.isNotEmpty)
                        Text(
                          'IP: ${d.ipAddress}',
                          style: TextStyle(fontSize: 12, color: AppTheme.textSecondary),
                        ),
                    ],
                  ),
                ),
                _StatusBadge(status: d.status),
                if (showApprovalActions) ...[
                  const SizedBox(width: 8),
                  ElevatedButton(
                    onPressed: () => _approveDevice(d.id),
                    child: Text(t(t('同意'))),
                  ),
                  const SizedBox(width: 8),
                  OutlinedButton(
                    onPressed: () => _blacklistDevice(d.id, d.name),
                    child: Text(t(t('拉黑'))),
                  ),
                ],
                if (showConnectedActions) ...[
                  const SizedBox(width: 8),
                  OutlinedButton(
                    onPressed: () => _disconnectDevice(d.id, d.name),
                    style: OutlinedButton.styleFrom(
                      side: BorderSide(color: AppTheme.warningColor),
                      foregroundColor: AppTheme.warningColor,
                    ),
                    child: Text(t(t('踢出'))),
                  ),
                  const SizedBox(width: 8),
                  OutlinedButton(
                    onPressed: () => _blacklistDevice(d.id, d.name),
                    style: OutlinedButton.styleFrom(
                      side: BorderSide(color: AppTheme.errorColor),
                      foregroundColor: AppTheme.errorColor,
                    ),
                    child: Text(t(t('拉黑'))),
                  ),
                ],
                if (showBlockedActions) ...[
                  const SizedBox(width: 8),
                  OutlinedButton(
                    onPressed: () => _unblockDevice(d.id, d.name),
                    style: OutlinedButton.styleFrom(
                      side: BorderSide(color: AppTheme.successColor),
                      foregroundColor: AppTheme.successColor,
                    ),
                    child: Text(t(t('解除'))),
                  ),
                ],
              ],
            ),
          ),
        );
      },
    );
  }

  IconData _platformIcon(String platform) {
    switch (platform) {
      case 'windows':
        return Icons.laptop_windows;
      case 'android':
        return Icons.phone_android;
      case 'linux':
        return Icons.computer;
      case 'ios':
        return Icons.phone_iphone;
      default:
        return Icons.devices;
    }
  }
}

class _StatusBadge extends StatelessWidget {
  final String status;
  const _StatusBadge({required this.status});

  @override
  Widget build(BuildContext context) {
    Color color;
    String label;
    switch (status) {
      case 'pending':
        color = AppTheme.warningColor;
        label = t(t('待审核'));
        break;
      case 'connected':
        color = AppTheme.successColor;
        label = t(t('已连接'));
        break;
      case 'approved':
        color = const Color(0xFF8B5CF6);
        label = t(t('已审批'));
        break;
      case 'rejected':
        color = AppTheme.errorColor;
        label = t(t('已拒绝'));
        break;
      case 'blacklisted':
        color = AppTheme.errorColor;
        label = t(t('已拉黑'));
        break;
      case 'disconnected':
        color = AppTheme.textSecondary;
        label = t(t('已断开'));
        break;
      default:
        color = AppTheme.textSecondary;
        label = status;
    }
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: color.withOpacity(0.15),
        borderRadius: BorderRadius.circular(20),
      ),
      child: Text(
        label,
        style: TextStyle(fontSize: 12, color: color, fontWeight: FontWeight.w500),
      ),
    );
  }
}
