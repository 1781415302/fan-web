import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:fan_web/api/api_client.dart';
import 'package:fan_web/api/bangumi_api.dart';
import 'package:fan_web/api/library_api.dart';
import 'package:fan_web/providers/anime_provider.dart';
import 'package:fan_web/providers/auth_provider.dart';
import 'package:fan_web/screens/anime_add_screen.dart';
import 'package:fan_web/theme/app_theme.dart';

import 'library_test_fakes.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late FakeAnimeApi animeApi;
  late FakeLibraryApi libraryApi;
  late FakeBangumiApi bangumiApi;

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    animeApi = FakeAnimeApi();
    libraryApi = FakeLibraryApi();
    bangumiApi = FakeBangumiApi();
  });

  Future<void> pumpAdd(WidgetTester tester) async {
    final prefs = await SharedPreferences.getInstance();
    final router = GoRouter(
      initialLocation: '/animes/new',
      routes: [
        GoRoute(
          path: '/',
          name: 'home',
          builder: (context, state) => const Scaffold(body: Text('home')),
        ),
        GoRoute(
          path: '/animes/new',
          name: 'animeAdd',
          builder: (context, state) => const AnimeAddScreen(),
        ),
        GoRoute(
          path: '/animes/:id',
          name: 'animeDetail',
          builder: (context, state) =>
              Scaffold(body: Text('detail-${state.pathParameters['id']}')),
        ),
      ],
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sharedPreferencesProvider.overrideWithValue(prefs),
          apiClientProvider.overrideWithValue(ApiClient()),
          authProvider.overrideWith(() => FixedAuthNotifier(adminAuthState())),
          animeApiProvider.overrideWithValue(animeApi),
          libraryApiProvider.overrideWithValue(libraryApi),
          bangumiApiProvider.overrideWithValue(bangumiApi),
        ],
        child: MaterialApp.router(
          theme: AppTheme.darkTheme,
          routerConfig: router,
        ),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));
  }

  Future<void> searchAndSelect(WidgetTester tester) async {
    await tester.enterText(find.byKey(const Key('bangumi-search')), 'show');
    await tester.tap(find.byKey(const Key('bangumi-search-submit')));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));
    await tester.tap(find.byKey(const Key('bangumi-item-101')));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));
  }

  testWidgets('can submit empty string for video root', (tester) async {
    await pumpAdd(tester);
    await searchAndSelect(tester);
    expect(find.text('视频根目录'), findsOneWidget);
    expect(find.byKey(const Key('dir-ShowDir')), findsOneWidget);
    await tester.ensureVisible(find.byKey(const Key('anime-add-submit')));
    await tester.tap(find.byKey(const Key('anime-add-submit')));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));
    expect(animeApi.lastCreateBangumiId, 101);
    expect(animeApi.lastCreateFilePath, '');
    expect(find.text('detail-42'), findsOneWidget);
  });

  testWidgets('can select a listed dir and submit it', (tester) async {
    await pumpAdd(tester);
    await searchAndSelect(tester);
    await tester.ensureVisible(find.byKey(const Key('dir-ShowDir')));
    await tester.tap(find.byKey(const Key('dir-ShowDir')));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));
    await tester.ensureVisible(find.byKey(const Key('anime-add-submit')));
    await tester.tap(find.byKey(const Key('anime-add-submit')));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));
    expect(animeApi.lastCreateFilePath, 'ShowDir');
  });
}
