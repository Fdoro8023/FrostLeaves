import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../theme/app_theme.dart';
import '../theme/theme_mode.dart';
import '../i18n/i18n.dart';

/// 外观模式选择：深色 / 浅色 / 跟随系统（默认深色）
class ThemeModeSelector extends ConsumerWidget {
  const ThemeModeSelector({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final mode = ref.watch(themeModeProvider);

    Widget option(ThemeMode m, String label, IconData icon) {
      final active = mode == m;
      return Expanded(
        child: InkWell(
          borderRadius: BorderRadius.circular(8),
          onTap: () {
            ref.read(themeModeProvider.notifier).state = m;
            persistThemeMode(m);
          },
          child: Container(
            margin: const EdgeInsets.symmetric(horizontal: 4),
            padding: const EdgeInsets.symmetric(vertical: 10),
            decoration: BoxDecoration(
              color: active
                  ? AppTheme.primaryColor.withOpacity(0.15)
                  : AppTheme.backgroundColor,
              borderRadius: BorderRadius.circular(8),
              border: Border.all(
                color: active ? AppTheme.primaryColor : AppTheme.borderColor,
              ),
            ),
            child: Column(
              children: [
                Icon(icon,
                    size: 18,
                    color: active
                        ? AppTheme.primaryColor
                        : AppTheme.textSecondary),
                const SizedBox(height: 4),
                Text(label,
                    style: TextStyle(
                        fontSize: 12,
                        color: active
                            ? AppTheme.primaryColor
                            : AppTheme.textSecondary)),
              ],
            ),
          ),
        ),
      );
    }

    return Row(
      children: [
        option(ThemeMode.dark, t('深色'), Icons.dark_mode_outlined),
        option(ThemeMode.light, t('浅色'), Icons.light_mode_outlined),
        option(ThemeMode.system, t('跟随系统'), Icons.brightness_auto_outlined),
      ],
    );
  }
}
