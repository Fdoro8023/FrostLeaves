import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../i18n/i18n.dart';
import '../theme/app_theme.dart';

/// 语言选择：中文（默认）/ English
class LanguageSelector extends ConsumerWidget {
  const LanguageSelector({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final en = ref.watch(languageProvider);

    Widget option(bool value, String label) {
      final active = en == value;
      return Expanded(
        child: InkWell(
          borderRadius: BorderRadius.circular(8),
          onTap: () {
            ref.read(languageProvider.notifier).state = value;
            L.apply(en: value);
            persistEnglish(value);
          },
          child: Container(
            margin: const EdgeInsets.symmetric(horizontal: 4),
            padding: const EdgeInsets.symmetric(vertical: 12),
            decoration: BoxDecoration(
              color: active
                  ? AppTheme.primaryColor.withOpacity(0.15)
                  : AppTheme.backgroundColor,
              borderRadius: BorderRadius.circular(8),
              border: Border.all(
                color: active ? AppTheme.primaryColor : AppTheme.borderColor,
              ),
            ),
            child: Center(
              child: Text(label,
                  style: TextStyle(
                      fontSize: 13,
                      color: active
                          ? AppTheme.primaryColor
                          : AppTheme.textSecondary)),
            ),
          ),
        ),
      );
    }

    return Row(
      children: [
        option(false, '中文'),
        option(true, 'English'),
      ],
    );
  }
}
