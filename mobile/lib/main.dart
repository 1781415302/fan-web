import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'providers/auth_provider.dart';
import 'router.dart';
import 'theme/app_theme.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  MediaKit.ensureInitialized();
  final preferences = await SharedPreferences.getInstance();

  runApp(
    ProviderScope(
      overrides: [sharedPreferencesProvider.overrideWithValue(preferences)],
      child: const FanWebApp(),
    ),
  );
}

class FanWebApp extends ConsumerStatefulWidget {
  const FanWebApp({super.key});

  @override
  ConsumerState<FanWebApp> createState() => _FanWebAppState();
}

class _FanWebAppState extends ConsumerState<FanWebApp> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        ref.read(authProvider.notifier).init();
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    return AnnotatedRegion<SystemUiOverlayStyle>(
      value: const SystemUiOverlayStyle(
        statusBarColor: Colors.transparent,
        statusBarIconBrightness: Brightness.light,
        systemNavigationBarColor: AppTheme.background,
        systemNavigationBarIconBrightness: Brightness.light,
      ),
      child: MaterialApp.router(
        title: 'fan-web',
        debugShowCheckedModeBanner: false,
        theme: AppTheme.darkTheme,
        routerConfig: ref.watch(routerProvider),
      ),
    );
  }
}
