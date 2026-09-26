import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'core/theme/app_theme.dart';
import 'core/theme/theme_mode.dart';
import 'core/i18n/i18n.dart';
import 'core/router/app_router.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final savedMode = await readSavedThemeMode(); // 默认深色
  final savedEn = await readSavedEnglish();     // 默认中文
  runApp(
    ProviderScope(
      overrides: [
        themeModeProvider.overrideWith((ref) => savedMode),
        languageProvider.overrideWith((ref) => savedEn),
      ],
      child: const FrostLeavesApp(),
    ),
  );
}

class FrostLeavesApp extends ConsumerWidget {
  const FrostLeavesApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final router = ref.watch(routerProvider);
    final mode = ref.watch(themeModeProvider);
    final en = ref.watch(languageProvider);

    final platformDark =
        MediaQuery.platformBrightnessOf(context) == Brightness.dark;
    final dark = mode == ThemeMode.dark ||
        (mode == ThemeMode.system && platformDark);
    AppTheme.applyMode(dark: dark);
    L.apply(en: en);

    return MaterialApp.router(
      title: 'FrostLeaves',
      debugShowCheckedModeBanner: false,
      theme: AppTheme.light(),
      darkTheme: AppTheme.dark(),
      themeMode: dark ? ThemeMode.dark : ThemeMode.light,
      routerConfig: router,
    );
  }
}