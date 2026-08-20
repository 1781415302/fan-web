import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:fan_web/api/anime_api.dart';
import 'package:fan_web/api/api_client.dart';
import 'package:fan_web/api/library_api.dart';
import 'package:fan_web/providers/anime_provider.dart';
import 'package:fan_web/providers/auth_provider.dart';
import 'package:fan_web/screens/anime_list_screen.dart';
import 'package:fan_web/theme/app_theme.dart';

import 'library_test_fakes.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late FakeAnimeApi animeApi;
  late FakeLibraryApi libraryApi;

  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    animeApi = FakeAnimeApi();
    libraryApi = FakeLibraryApi();
  });

  Future<void> pumpList(WidgetTester tester, {required AuthState auth}) async {
    final prefs = await SharedPreferences.getInstance();
    final router = GoRouter(
      initialLocation: '/',
      routes: [
        GoRoute(
          path: '/',
          name: 'home',
          builder: (context, state) => const AnimeListScreen(),
        ),
        GoRoute(
          path: '/animes/new',
          name: 'animeAdd',
          builder: (context, state) => const Scaffold(body: Text('add-page')),
        ),
        GoRoute(
          path: '/animes/:id',
          name: 'animeDetail',
          builder: (context, state) =>
              const Scaffold(body: Text('detail-page')),
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
          libraryApiProvider.overrideWithValue(libraryApi),
        ],
        child: MaterialApp.router(
          theme: AppTheme.darkTheme,
          routerConfig: router,
        ),
      ),
    );
    await tester.pump();
    await tester.pump();
  }

  testWidgets('non-admin does not see scan or add', (tester) async {
    await pumpList(tester, auth: userAuthState());
    expect(find.text('库扫描'), findsNothing);
    expect(find.text('添加番剧'), findsNothing);
    expect(find.byKey(const Key('library-scan')), findsNothing);
    expect(find.byKey(const Key('anime-add')), findsNothing);
  });

  testWidgets('admin sees scan and add', (tester) async {
    await pumpList(tester, auth: adminAuthState());
    expect(find.text('库扫描'), findsOneWidget);
    expect(find.text('添加番剧'), findsOneWidget);
  });

  testWidgets('admin confirm creates then scans', (tester) async {
    libraryApi.unidentified = [sampleUnidentified()];
    await pumpList(tester, auth: adminAuthState());
    await tester.pumpAndSettle();
    expect(find.text('ep01.mkv'), findsOneWidget);
    await tester.tap(find.textContaining('候选番剧'));
    await tester.pumpAndSettle();
    expect(animeApi.calls.where((item) => item.startsWith('create:')), [
      'create:101:ShowDir',
    ]);
    expect(animeApi.calls.where((item) => item.startsWith('scanAnime:')), [
      'scanAnime:42',
    ]);
    expect(
      animeApi.calls.indexOf('create:101:ShowDir'),
      lessThan(animeApi.calls.indexOf('scanAnime:42')),
    );
  });

  testWidgets('create 1001 does not scan', (tester) async {
    libraryApi.unidentified = [sampleUnidentified()];
    animeApi.createError = const ApiException(1001, '番剧已存在但目录不同');
    await pumpList(tester, auth: adminAuthState());
    await tester.pumpAndSettle();
    await tester.tap(find.textContaining('候选番剧'));
    await tester.pumpAndSettle();
    expect(find.text('番剧已存在但目录不同'), findsOneWidget);
    expect(
      animeApi.calls.where((item) => item.startsWith('scanAnime:')),
      isEmpty,
    );
    expect(find.text('ep01.mkv'), findsOneWidget);
  });

  testWidgets('search calls list with keyword', (tester) async {
    await pumpList(tester, auth: userAuthState());
    await tester.enterText(find.byKey(const Key('anime-search')), '进击');
    await tester.pump(const Duration(milliseconds: 350));
    await tester.pumpAndSettle();
    expect(animeApi.lastKeyword, '进击');
    expect(animeApi.calls, contains('list:进击'));
  });
}
