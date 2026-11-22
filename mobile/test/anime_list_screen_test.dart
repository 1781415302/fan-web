import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:shared_preferences/shared_preferences.dart';

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

  testWidgets('admin confirm calls confirmInbox once without create/scan', (
    tester,
  ) async {
    libraryApi.unidentified = [sampleUnidentified()];
    await pumpList(tester, auth: adminAuthState());
    await tester.pumpAndSettle();
    expect(find.text('ep01.mkv'), findsOneWidget);
    await tester.tap(find.textContaining('候选番剧'));
    await tester.pumpAndSettle();
    expect(libraryApi.confirmCalls, hasLength(1));
    final call = libraryApi.confirmCalls.single;
    expect(call.bangumiId, 101);
    expect(call.filePath, 'ShowDir');
    expect(call.fileNames, ['ep01.mkv']);
    expect(call.episodeOffset, 0);
    // 旧两步式 create + scan 不再调用。
    expect(
      animeApi.calls.where(
        (item) => item.startsWith('create:') || item.startsWith('scanAnime:'),
      ),
      isEmpty,
    );
    expect(find.text('ep01.mkv'), findsNothing);
  });

  testWidgets(
    'confirm collects same-dir files whose candidates contain the bangumi',
    (tester) async {
      const other = MatchCandidate(
        id: 202,
        name: 'Other',
        nameCn: '其他番剧',
        score: 0.8,
      );
      libraryApi.unidentified = [
        sampleUnidentified(fileName: 'ep01.mkv'),
        sampleUnidentified(fileName: 'ep02.mkv'),
        sampleUnidentified(fileName: 'ep03.mkv', candidates: const [other]),
        sampleUnidentified(fileName: 'ep04.mkv', filePath: 'OtherDir'),
      ];
      await pumpList(tester, auth: adminAuthState());
      await tester.pumpAndSettle();
      // 点击第二个文件的候选：被点击文件本身必须包含在内。
      await tester.tap(find.byKey(const Key('candidate-ShowDir-101')).at(1));
      await tester.pumpAndSettle();
      expect(libraryApi.confirmCalls, hasLength(1));
      final call = libraryApi.confirmCalls.single;
      expect(call.bangumiId, 101);
      expect(call.filePath, 'ShowDir');
      expect(call.fileNames, unorderedEquals(['ep01.mkv', 'ep02.mkv']));
      expect(call.fileNames, contains('ep02.mkv'));
      // 已确认文件移除；同目录候选不含该番剧的文件与其他目录文件保留。
      expect(find.text('ep01.mkv'), findsNothing);
      expect(find.text('ep02.mkv'), findsNothing);
      expect(find.text('ep03.mkv'), findsOneWidget);
      expect(find.text('ep04.mkv'), findsOneWidget);
    },
  );

  testWidgets('confirmInbox failure keeps every item and shows the error', (
    tester,
  ) async {
    libraryApi.unidentified = [
      sampleUnidentified(fileName: 'ep01.mkv'),
      sampleUnidentified(fileName: 'ep02.mkv'),
    ];
    libraryApi.confirmError = const ConfirmInboxException(
      1001,
      '番剧已存在但目录不同',
      reason: 'dir_mismatch',
    );
    await pumpList(tester, auth: adminAuthState());
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('candidate-ShowDir-101')).first);
    await tester.pumpAndSettle();
    expect(libraryApi.confirmCalls, hasLength(1));
    expect(find.text('番剧已存在但目录不同'), findsOneWidget);
    expect(find.text('ep01.mkv'), findsOneWidget);
    expect(find.text('ep02.mkv'), findsOneWidget);
    expect(
      animeApi.calls.where(
        (item) => item.startsWith('create:') || item.startsWith('scanAnime:'),
      ),
      isEmpty,
    );
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
