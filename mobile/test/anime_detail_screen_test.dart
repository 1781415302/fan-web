import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:fan_web/api/api_client.dart';
import 'package:fan_web/providers/anime_provider.dart';
import 'package:fan_web/providers/auth_provider.dart';
import 'package:fan_web/screens/anime_detail_screen.dart';
import 'package:fan_web/theme/app_theme.dart';

import 'library_test_fakes.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late FakeAnimeApi animeApi;
  late FakeProgressApi progressApi;

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    animeApi = FakeAnimeApi();
    progressApi = FakeProgressApi();
  });

  Future<void> pumpDetail(
    WidgetTester tester, {
    required AuthState auth,
  }) async {
    final prefs = await SharedPreferences.getInstance();
    final router = GoRouter(
      initialLocation: '/animes/7',
      routes: [
        GoRoute(
          path: '/',
          name: 'home',
          builder: (context, state) => const Scaffold(body: Text('home')),
        ),
        GoRoute(
          path: '/animes/:id',
          name: 'animeDetail',
          builder: (context, state) => const AnimeDetailScreen(animeId: 7),
        ),
        GoRoute(
          path: '/animes/:id/watch/:epId',
          name: 'watch',
          builder: (context, state) => const Scaffold(body: Text('watch')),
        ),
      ],
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sharedPreferencesProvider.overrideWithValue(prefs),
          apiClientProvider.overrideWithValue(ApiClient()),
          authProvider.overrideWith(() => FixedAuthNotifier(auth)),
          animeApiProvider.overrideWithValue(animeApi),
          progressApiProvider.overrideWithValue(progressApi),
        ],
        child: MaterialApp.router(
          theme: AppTheme.darkTheme,
          routerConfig: router,
        ),
      ),
    );
    await tester.pump();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));
  }

  testWidgets('admin sees delete and rescan, no rebind or edit form', (
    tester,
  ) async {
    await pumpDetail(tester, auth: adminAuthState());
    expect(find.text('扫描文件'), findsWidgets);
    expect(find.text('删除番剧'), findsWidgets);
    expect(find.text('重绑'), findsNothing);
    expect(find.text('重绑定'), findsNothing);
    expect(find.text('编辑信息'), findsNothing);
    expect(find.text('编辑'), findsNothing);
  });

  testWidgets('non-admin does not see delete or rescan', (tester) async {
    await pumpDetail(tester, auth: userAuthState());
    expect(find.text('扫描文件'), findsNothing);
    expect(find.text('删除番剧'), findsNothing);
    expect(find.byKey(const Key('anime-rescan')), findsNothing);
    expect(find.byKey(const Key('anime-delete')), findsNothing);
  });

  testWidgets('admin rescan calls scanAnime', (tester) async {
    await pumpDetail(tester, auth: adminAuthState());
    await tester.tap(find.byKey(const Key('anime-rescan')));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));
    expect(animeApi.calls, contains('scanAnime:7'));
  });
}
