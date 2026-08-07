import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'providers/auth_provider.dart';
import 'router.dart';
import 'services/progress_outbox.dart';
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

class _FanWebAppState extends ConsumerState<FanWebApp>
    with WidgetsBindingObserver {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        ref.read(authProvider.notifier).init();
      }
    });
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _onAppResumed();
    }
  }

  void _onAppResumed() {
    final auth = ref.read(authProvider);
    // 会话降级时尝试恢复
    if (auth.isSessionDegraded) {
      unawaited(ref.read(authProvider.notifier).retrySession());
    }
    // 尝试同步 outbox 中的待上传进度
    final user = auth.user;
    final serverUrl = auth.serverUrl;
    final token = auth.token;
    if (user != null && serverUrl != null && token != null && token.isNotEmpty) {
      final outbox = ref.read(progressOutboxProvider);
      unawaited(outbox.syncAll(serverUrl, user.id, token));
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
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
