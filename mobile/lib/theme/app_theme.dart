import 'package:flutter/material.dart';

abstract final class AppTheme {
  static const background = Color(0xFF0F0F23);
  static const foreground = Color(0xFFF8FAFC);
  static const muted = Color(0xFF27273B);
  static const coverPlaceholder = Color(0xFF35354A);
  static const accent = Color(0xFF22C55E);
  static const accentHover = Color(0xFF4ADE80);
  static const border = Color(0xFF312E81);
  static const destructive = Color(0xFFEF4444);
  static const warning = Color(0xFFF59E0B);

  static final _dividerColor = border.withValues(alpha: 0.5);

  static final darkTheme = ThemeData(
    useMaterial3: true,
    brightness: Brightness.dark,
    fontFamily: 'Inter',
    scaffoldBackgroundColor: background,
    dividerColor: _dividerColor,
    // Android 预测性返回在手指越过提交阈值后可能已经把页面推进到过渡终点；
    // 使用普通 Material 返回过渡，确保松手时始终有完整的退出动画。
    pageTransitionsTheme: PageTransitionsTheme(
      builders: {
        TargetPlatform.android: FadeForwardsPageTransitionsBuilder(
          backgroundColor: background,
        ),
      },
    ),
    colorScheme:
        ColorScheme.fromSeed(
          seedColor: accent,
          brightness: Brightness.dark,
        ).copyWith(
          primary: accent,
          onPrimary: background,
          secondary: border,
          onSecondary: foreground,
          surface: muted,
          onSurface: foreground,
          outline: border,
          error: destructive,
          onError: Colors.white,
        ),
    appBarTheme: const AppBarTheme(
      backgroundColor: background,
      foregroundColor: foreground,
      centerTitle: false,
      elevation: 0,
      scrolledUnderElevation: 0,
    ),
    textTheme: TextTheme(
      displayLarge: const TextStyle(
        color: foreground,
        fontWeight: FontWeight.w700,
      ),
      displayMedium: const TextStyle(
        color: foreground,
        fontWeight: FontWeight.w700,
      ),
      displaySmall: const TextStyle(
        color: foreground,
        fontWeight: FontWeight.w700,
      ),
      headlineMedium: const TextStyle(
        color: foreground,
        fontWeight: FontWeight.w600,
      ),
      titleLarge: const TextStyle(
        color: foreground,
        fontWeight: FontWeight.w600,
      ),
      titleMedium: const TextStyle(
        color: foreground,
        fontWeight: FontWeight.w600,
      ),
      bodyLarge: const TextStyle(color: foreground),
      bodyMedium: TextStyle(color: foreground.withValues(alpha: 0.85)),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: muted,
      contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 16),
      border: OutlineInputBorder(
        borderRadius: const BorderRadius.all(Radius.circular(8)),
        borderSide: BorderSide(color: border),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: const BorderRadius.all(Radius.circular(8)),
        borderSide: BorderSide(color: border),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: const BorderRadius.all(Radius.circular(8)),
        borderSide: BorderSide(color: accent, width: 2),
      ),
      errorBorder: OutlineInputBorder(
        borderRadius: const BorderRadius.all(Radius.circular(8)),
        borderSide: BorderSide(color: destructive),
      ),
      focusedErrorBorder: OutlineInputBorder(
        borderRadius: const BorderRadius.all(Radius.circular(8)),
        borderSide: BorderSide(color: destructive, width: 2),
      ),
      labelStyle: const TextStyle(color: foreground),
      prefixIconColor: foreground,
    ),
    cardTheme: const CardThemeData(
      color: muted,
      surfaceTintColor: Colors.transparent,
      margin: EdgeInsets.zero,
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.all(Radius.circular(12)),
        side: BorderSide(color: border),
      ),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: ButtonStyle(
        minimumSize: const WidgetStatePropertyAll(Size.fromHeight(52)),
        shape: const WidgetStatePropertyAll(
          RoundedRectangleBorder(
            borderRadius: BorderRadius.all(Radius.circular(8)),
          ),
        ),
        backgroundColor: const WidgetStatePropertyAll(accent),
        foregroundColor: const WidgetStatePropertyAll(background),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: ButtonStyle(
        minimumSize: const WidgetStatePropertyAll(Size.fromHeight(48)),
        shape: const WidgetStatePropertyAll(
          RoundedRectangleBorder(
            borderRadius: BorderRadius.all(Radius.circular(8)),
          ),
        ),
        foregroundColor: const WidgetStatePropertyAll(foreground),
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: ButtonStyle(foregroundColor: const WidgetStatePropertyAll(accent)),
    ),
    iconTheme: const IconThemeData(color: foreground),
    snackBarTheme: const SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      backgroundColor: muted,
      contentTextStyle: TextStyle(color: foreground),
    ),
    dialogTheme: const DialogThemeData(
      backgroundColor: muted,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.all(Radius.circular(12)),
      ),
    ),
    bottomSheetTheme: const BottomSheetThemeData(
      backgroundColor: muted,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
    ),
    dividerTheme: DividerThemeData(
      color: _dividerColor,
      thickness: 1,
      space: 1,
    ),
    progressIndicatorTheme: const ProgressIndicatorThemeData(color: accent),
    listTileTheme: const ListTileThemeData(
      iconColor: foreground,
      textColor: foreground,
    ),
  );
}
