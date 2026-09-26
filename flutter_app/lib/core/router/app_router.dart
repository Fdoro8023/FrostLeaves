import '../../core/i18n/i18n.dart';
import 'dart:io';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../features/server/pages/server_dashboard_page.dart';
import '../../features/server/pages/device_management_page.dart';
import '../../features/server/pages/server_file_manager_page.dart';
import '../../features/server/pages/server_settings_page.dart';
import '../../features/server/pages/audit_log_page.dart';
import '../../features/server/pages/share_management_page.dart';
import '../../features/server/pages/about_page.dart';
import '../../features/client/pages/client_connect_page.dart';
import '../../features/client/pages/client_file_browser_page.dart';
import '../../features/client/pages/client_settings_page.dart';
import '../../features/shared/pages/mode_select_page.dart';
import '../../features/shared/pages/server_shell_page.dart';
import '../../features/shared/pages/client_shell_page.dart';
import '../utils/platform_helper.dart';

final routerProvider = Provider<GoRouter>((ref) {
  return GoRouter(
    initialLocation: '/splash',
    routes: [
      GoRoute(
        path: '/splash',
        builder: (_, __) => const ModeSelectPage(),
      ),
      // Server mode (Windows only)
      ShellRoute(
        builder: (_, __, child) => ServerShellPage(child: child),
        routes: [
          GoRoute(
            path: '/server/dashboard',
            builder: (_, __) => const ServerDashboardPage(),
          ),
          GoRoute(
            path: '/server/devices',
            builder: (_, __) => const DeviceManagementPage(),
          ),
          GoRoute(
            path: '/server/files',
            builder: (_, __) => const ServerFileManagerPage(),
          ),
          GoRoute(
            path: '/server/settings',
            builder: (_, __) => const ServerSettingsPage(),
          ),
          GoRoute(
            path: '/server/audit',
            builder: (_, __) => const AuditLogPage(),
          ),
          GoRoute(
            path: '/server/share',
            builder: (_, __) => const ShareManagementPage(),
          ),
          GoRoute(
            path: '/server/about',
            builder: (_, __) => const AboutPage(),
          ),
        ],
      ),
      // Client mode (all platforms)
      ShellRoute(
        builder: (_, __, child) => ClientShellPage(child: child),
        routes: [
          GoRoute(
            path: '/client/connect',
            builder: (_, __) => const ClientConnectPage(),
          ),
          GoRoute(
            path: '/client/files',
            builder: (_, __) => const ClientFileBrowserPage(),
          ),
          GoRoute(
            path: '/client/settings',
            builder: (_, __) => const ClientSettingsPage(),
          ),
        ],
      ),
    ],
  );
});
