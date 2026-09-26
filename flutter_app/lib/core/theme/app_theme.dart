import 'package:flutter/material.dart';

/// 主题调色板：深色（默认）/ 浅色，可通过设置切换。
/// 颜色以 getter 暴露，调用点无需改动；切换时 MaterialApp 重建即生效。
class AppTheme {
  static bool _dark = true;
  static bool get isDark => _dark;

  /// 由 App 根节点按当前模式调用
  static void applyMode({required bool dark}) => _dark = dark;

  // ===== 深色 =====\n
  static const Color _dPrimary = Color(0xFFD97757);
  static const Color _dBackground = Color(0xFF1A1A1A);
  static const Color _dSurface = Color(0xFF242424);
  static const Color _dCard = Color(0xFF2D2D2D);
  static const Color _dBorder = Color(0xFF3D3D3D);
  static const Color _dTextPrimary = Color(0xFFF5F5F5);
  static const Color _dTextSecondary = Color(0xFFA0A0A0);
  static const Color _dSuccess = Color(0xFF4CAF50);
  static const Color _dError = Color(0xFFE57373);
  static const Color _dWarning = Color(0xFFFFB74D);

  // ===== 浅色 =====\n
  static const Color _lPrimary = Color(0xFFC4653F);
  static const Color _lBackground = Color(0xFFFAF9F7);
  static const Color _lSurface = Color(0xFFF2F0EC);
  static const Color _lCard = Color(0xFFFFFFFF);
  static const Color _lBorder = Color(0xFFDCD8D2);
  static const Color _lTextPrimary = Color(0xFF1F1E1D);
  static const Color _lTextSecondary = Color(0xFF6B6763);
  static const Color _lSuccess = Color(0xFF2E7D32);
  static const Color _lError = Color(0xFFC62828);
  static const Color _lWarning = Color(0xFFE65100);

  static Color get primaryColor => _dark ? _dPrimary : _lPrimary;
  static Color get backgroundColor => _dark ? _dBackground : _lBackground;
  static Color get surfaceColor => _dark ? _dSurface : _lSurface;
  static Color get cardColor => _dark ? _dCard : _lCard;
  static Color get borderColor => _dark ? _dBorder : _lBorder;
  static Color get textPrimary => _dark ? _dTextPrimary : _lTextPrimary;
  static Color get textSecondary => _dark ? _dTextSecondary : _lTextSecondary;
  static Color get successColor => _dark ? _dSuccess : _lSuccess;
  static Color get errorColor => _dark ? _dError : _lError;
  static Color get warningColor => _dark ? _dWarning : _lWarning;

  /// 浅色主题
  static ThemeData light() => _build(Brightness.light);

  /// 深色主题
  static ThemeData dark() => _build(Brightness.dark);

  static ThemeData _build(Brightness brightness) {
    final isDark = brightness == Brightness.dark;
    final primary = primaryColor;
    final bg = backgroundColor;
    final surface = surfaceColor;
    final card = cardColor;
    final border = borderColor;
    final tPrimary = textPrimary;
    final tSecondary = textSecondary;
    final err = errorColor;

    return ThemeData(
      brightness: brightness,
      primaryColor: primary,
      scaffoldBackgroundColor: bg,
      cardColor: card,
      colorScheme: isDark
          ? ColorScheme.dark(
              primary: primary,
              secondary: const Color(0xFF8B5CF6),
              surface: surface,
              error: err,
              onPrimary: Colors.white,
              onSecondary: Colors.white,
              onSurface: tPrimary,
              onError: Colors.white,
            )
          : ColorScheme.light(
              primary: primary,
              secondary: const Color(0xFF7C3AED),
              surface: surface,
              error: err,
              onPrimary: Colors.white,
              onSecondary: Colors.white,
              onSurface: tPrimary,
              onError: Colors.white,
            ),
      appBarTheme: AppBarTheme(
        backgroundColor: bg,
        elevation: 0,
        centerTitle: false,
        titleTextStyle: TextStyle(
          color: tPrimary,
          fontSize: 20,
          fontWeight: FontWeight.w600,
          letterSpacing: -0.5,
        ),
        iconTheme: IconThemeData(color: tPrimary),
      ),
      cardTheme: CardTheme(
        color: card,
        elevation: 0,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(12),
          side: BorderSide(color: border, width: 1),
        ),
      ),
      elevatedButtonTheme: ElevatedButtonThemeData(
        style: ElevatedButton.styleFrom(
          backgroundColor: primary,
          foregroundColor: Colors.white,
          elevation: 0,
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 14),
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
          textStyle: const TextStyle(fontSize: 15, fontWeight: FontWeight.w600),
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          foregroundColor: tPrimary,
          side: BorderSide(color: border),
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 14),
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          foregroundColor: primary,
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
        ),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: surface,
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(10),
          borderSide: BorderSide(color: border),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(10),
          borderSide: BorderSide(color: border),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(10),
          borderSide: BorderSide(color: primary, width: 2),
        ),
        errorBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(10),
          borderSide: BorderSide(color: err),
        ),
        contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
        hintStyle: TextStyle(color: tSecondary),
      ),
      dividerTheme: DividerThemeData(color: border, thickness: 1, space: 1),
      listTileTheme: ListTileThemeData(textColor: tPrimary, iconColor: tSecondary),
      bottomNavigationBarTheme: BottomNavigationBarThemeData(
        backgroundColor: surface,
        selectedItemColor: primary,
        unselectedItemColor: tSecondary,
        type: BottomNavigationBarType.fixed,
        elevation: 0,
      ),
      snackBarTheme: SnackBarThemeData(
        backgroundColor: card,
        contentTextStyle: TextStyle(color: tPrimary),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
        behavior: SnackBarBehavior.floating,
      ),
      dialogTheme: DialogTheme(
        backgroundColor: card,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
        titleTextStyle: TextStyle(
          color: tPrimary,
          fontSize: 18,
          fontWeight: FontWeight.w600,
        ),
      ),
    );
  }
}