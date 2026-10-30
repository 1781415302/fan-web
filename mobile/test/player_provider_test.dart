import 'dart:async';

import 'package:flutter/gestures.dart' show kLongPressTimeout;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_riverpod/misc.dart' show Override;
import 'package:flutter_test/flutter_test.dart';
import 'package:media_kit/media_kit.dart' as mk;
import 'package:media_kit_video/media_kit_video.dart' as mkv;
import 'package:shared_preferences/shared_preferences.dart';

import 'package:fan_web/api/api_client.dart';
import 'package:fan_web/api/media_api.dart';
import 'package:fan_web/models/anime.dart';
import 'package:fan_web/models/download.dart';
import 'package:fan_web/providers/anime_provider.dart';
import 'package:fan_web/providers/auth_provider.dart';
import 'package:fan_web/providers/download_provider.dart';
import 'package:fan_web/models/playback_interaction.dart' as model;
import 'package:fan_web/providers/player_provider.dart';
import 'package:fan_web/screens/player_screen.dart';
import 'package:fan_web/widgets/player_gesture_surface.dart';

import 'library_test_fakes.dart';

void main() {
  group('buildPlayerMedia', () {
    test('uses a media token and preserves resume position', () async {
      final built = await buildPlayerMedia(
        requestMediaToken: (episodeId) async {
          expect(episodeId, 7);
          return const MediaTokenResult(
            token: 'media+/token',
            expiresAt: '2026-08-17T12:00:00Z',
          );
        },
        serverUrl: 'http://127.0.0.1:8080/',
        episodeId: 7,
        startPositionSeconds: 125,
      );

      expect(
        built.media.uri.toString(),
        'http://127.0.0.1:8080/api/episodes/7/stream?media_token=media%2B%2Ftoken',
      );
      expect(
        built.media.uri.toString(),
        isNot(contains('LOGIN_MUST_NOT_APPEAR')),
      );
      expect(built.media.uri.toString(), isNot(contains('?token=')));
      expect(built.media.start, const Duration(seconds: 125));
      expect(built.expiresAt, DateTime.parse('2026-08-17T12:00:00Z'));
    });

    test('treats MediaTokenUnsupported as a hard fail', () async {
      String? url;
      await expectLater(() async {
        final built = await buildPlayerMedia(
          requestMediaToken: (_) async => throw const MediaTokenUnsupported(),
          serverUrl: 'http://127.0.0.1:8080',
          episodeId: 7,
          startPositionSeconds: 0,
        );
        url = built.media.uri.toString();
        return built.media;
      }, throwsA(isA<MediaTokenUnsupported>()));
      expect(url, isNull);
      expect(url ?? '', isNot(contains('token=')));
    });

    for (final error in <Object>[
      const ApiException(2001, '未登录'),
      const ApiException(9999, '服务器错误'),
      TimeoutException('timeout'),
      const FormatException('bad response'),
    ]) {
      test('does not fall back for ${error.runtimeType}', () async {
        expect(
          () => buildPlayerMedia(
            requestMediaToken: (_) async => throw error,
            serverUrl: 'http://127.0.0.1:8080',
            episodeId: 7,
            startPositionSeconds: 0,
          ),
          throwsA(same(error)),
        );
      });
    }
  });

  group('shouldQueueProgress', () {
    test('dispose-0 sentinel must not queue', () {
      expect(shouldQueueProgress(0), isFalse);
      expect(shouldQueueProgress(-1), isFalse);
    });

    test('positive position may queue', () {
      expect(shouldQueueProgress(1), isTrue);
      expect(shouldQueueProgress(590), isTrue);
    });
  });

  group('playbackRateOptions', () {
    test('exposes five rates and no 0.75', () {
      expect(playbackRateOptions, <double>[0.5, 1, 1.25, 1.5, 2]);
      expect(playbackRateOptions.contains(0.75), isFalse);
    });

    test('unknown rate falls back to 1 and is not persisted', () {
      expect(normalizePlaybackRate(0.75), 1);
      expect(normalizePlaybackRate(3), 1);
      expect(normalizePlaybackRate(1.25), 1.25);
      expect(playbackRateLabel(1), '1x');
      expect(playbackRateLabel(1.25), '1.25x');
    });
  });

  group('nextEpisodeOf', () {
    const first = Episode(
      id: 10,
      animeId: 1,
      epNumber: 1,
      title: '01',
      filePath: '01.mkv',
      duration: 0,
    );
    const second = Episode(
      id: 11,
      animeId: 1,
      epNumber: 2,
      title: '02',
      filePath: '02.mkv',
      duration: 0,
    );

    test('returns the next episode id when present', () {
      expect(nextEpisodeOf([first, second], 10)?.id, 11);
    });

    test('returns null on the last episode', () {
      expect(nextEpisodeOf([first, second], 11), isNull);
      expect(nextEpisodeOf([first], 10), isNull);
    });
  });

  group('prevEpisodeOf', () {
    const first = Episode(
      id: 10,
      animeId: 1,
      epNumber: 1,
      title: '01',
      filePath: '01.mkv',
      duration: 0,
    );
    const second = Episode(
      id: 11,
      animeId: 1,
      epNumber: 2,
      title: '02',
      filePath: '02.mkv',
      duration: 0,
    );

    test('returns the previous episode id when present', () {
      expect(prevEpisodeOf([first, second], 11)?.id, 10);
    });

    test('returns null on the first episode', () {
      expect(prevEpisodeOf([first, second], 10), isNull);
      expect(prevEpisodeOf([first], 10), isNull);
    });
  });
  group('PlayerState temporary rate', () {
    test('defaults to no temporary rate and keeps normal rate 1', () {
      const state = PlayerState();
      expect(state.playbackRate, 1);
      expect(state.temporaryPlaybackRate, isNull);
      expect(state.isTemporaryBoosting, isFalse);
      expect(state.effectivePlaybackRate, 1);
    });

    test('effective rate prefers temporary over normal selection', () {
      const state = PlayerState(playbackRate: 1.25, temporaryPlaybackRate: 3);
      expect(state.isTemporaryBoosting, isTrue);
      expect(state.effectivePlaybackRate, 3);
      expect(state.playbackRate, 1.25);
    });

    test('copyWith keeps temporary rate when not passed', () {
      const state = PlayerState(playbackRate: 2, temporaryPlaybackRate: 1.5);
      final next = state.copyWith(isPlaying: true, position: Duration.zero);
      expect(next.temporaryPlaybackRate, 1.5);
      expect(next.playbackRate, 2);
      expect(next.effectivePlaybackRate, 1.5);
    });

    test('copyWith clears temporary rate with the clear flag', () {
      const state = PlayerState(playbackRate: 2, temporaryPlaybackRate: 1.5);
      final next = state.copyWith(clearTemporaryPlaybackRate: true);
      expect(next.temporaryPlaybackRate, isNull);
      expect(next.isTemporaryBoosting, isFalse);
      expect(next.effectivePlaybackRate, 2);
    });

    test('copyWith value wins over clear flag', () {
      const state = PlayerState(temporaryPlaybackRate: 2);
      final next = state.copyWith(
        temporaryPlaybackRate: 2.5,
        clearTemporaryPlaybackRate: true,
      );
      expect(next.temporaryPlaybackRate, 2.5);
    });

    test('copyWith preserves existing nullable clear flags', () {
      const state = PlayerState(
        error: 'e',
        notice: 'n',
        restoredPosition: 10,
        temporaryPlaybackRate: 2,
      );
      final next = state.copyWith(clearError: true, clearNotice: true);
      expect(next.error, isNull);
      expect(next.notice, isNull);
      expect(next.restoredPosition, 10);
      expect(next.temporaryPlaybackRate, 2);
    });
  });

  group('player_provider rate re-export', () {
    test('re-exports the shared normal rate definitions', () {
      expect(identical(playbackRateOptions, model.playbackRateOptions), isTrue);
      expect(normalizePlaybackRate(3), model.normalizePlaybackRate(3));
      expect(playbackRateLabel(3), '1x');
      expect(model.temporaryPlaybackRateLabel(3), '3x');
    });
  });

  _sessionWiringTests();
  _seekByTests();
  _skipSeekWiringTests();
}

// ── 生产 Notifier / PlayerScreen 接线层测试：轻量原生替身，不加载 libmpv ──

/// 记录型原生 Player 替身：open/seek 可用 Completer 阻塞。
class _FakeNativePlayer implements mk.Player {
  final playingController = StreamController<bool>.broadcast();
  final positionController = StreamController<Duration>.broadcast();
  final errorController = StreamController<String>.broadcast();
  // 默认不 emit：既有用例中 state.duration 仍为 0、未完成。
  final durationController = StreamController<Duration>.broadcast();
  final completedController = StreamController<bool>.broadcast();

  int openCount = 0;
  int playCount = 0;
  int pauseCount = 0;
  final List<Duration> seeks = <Duration>[];
  final List<double> rateWrites = <double>[];
  Completer<void>? openGate;
  Completer<void>? seekGate;

  bool _playing = false;
  Duration _position = Duration.zero;

  void setPosition(Duration position) {
    _position = position;
    positionController.add(position);
  }

  @override
  mk.PlayerState get state => mk.PlayerState(
    playing: _playing,
    position: _position,
    duration: const Duration(minutes: 24),
  );

  @override
  late final mk.PlayerStream stream = mk.PlayerStream(
    const Stream.empty(),
    playingController.stream,
    completedController.stream,
    positionController.stream,
    durationController.stream,
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    const Stream.empty(),
    errorController.stream,
  );

  @override
  mk.PlatformPlayer? get platform => null;

  @override
  Future<void> open(mk.Playable playable, {bool play = true}) async {
    openCount++;
    final gate = openGate;
    if (gate != null) {
      await gate.future;
    }
  }

  @override
  Future<void> play() async {
    playCount++;
    _playing = true;
    playingController.add(true);
  }

  @override
  Future<void> pause() async {
    pauseCount++;
    _playing = false;
    playingController.add(false);
  }

  @override
  Future<void> seek(Duration duration) async {
    seeks.add(duration);
    final gate = seekGate;
    if (gate != null) {
      await gate.future;
    }
    _position = duration;
  }

  @override
  Future<void> setRate(double rate) async {
    rateWrites.add(rate);
  }

  @override
  Future<void> setSubtitleTrack(mk.SubtitleTrack track) async {}

  @override
  Future<void> dispose() async {}

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _FakeVideoController implements mkv.VideoController {
  _FakeVideoController(this.player);

  @override
  final mk.Player player;

  @override
  final notifier = ValueNotifier<mkv.PlatformVideoController?>(null);

  @override
  final ValueNotifier<int?> id = ValueNotifier<int?>(null);

  @override
  final ValueNotifier<Rect?> rect = ValueNotifier<Rect?>(null);

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _FakeMediaApi extends MediaApi {
  _FakeMediaApi() : super(ApiClient());

  int fetchCount = 0;
  Completer<void>? fetchGate;
  Object? fetchError;

  @override
  Future<MediaTokenResult> fetchMediaToken(int episodeId) async {
    fetchCount++;
    final gate = fetchGate;
    if (gate != null) {
      await gate.future;
    }
    final error = fetchError;
    if (error != null) {
      throw error;
    }
    // 30 秒内过期：播放器 error 时走临近过期刷新与 15 秒守卫路径。
    return MediaTokenResult(
      token: 'media-$fetchCount',
      expiresAt: DateTime.now()
          .add(const Duration(seconds: 30))
          .toUtc()
          .toIso8601String(),
    );
  }
}

class _SessionProgressApi extends FakeProgressApi {
  @override
  Future<EpisodeProgress> getEpisodeProgress(int episodeId) async {
    return EpisodeProgress(
      episodeId: episodeId,
      position: 0,
      watched: false,
      updatedAt: '',
    );
  }

  @override
  Future<void> reportProgress(
    int episodeId,
    int position,
    bool watched, {
    String? token,
  }) async {}
}

class _NoDownloadNotifier extends DownloadNotifier {
  @override
  Map<DownloadKey, DownloadTaskState> build() => const {};

  @override
  String? localFilePath(String serverUrl, int episodeId) => null;
}

class _Natives {
  final List<_FakeNativePlayer> players = <_FakeNativePlayer>[];
  final _FakeMediaApi mediaApi = _FakeMediaApi();

  Future<List<Override>> overrides() async {
    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    return [
      sharedPreferencesProvider.overrideWithValue(prefs),
      apiClientProvider.overrideWithValue(ApiClient()),
      authProvider.overrideWith(() => FixedAuthNotifier(userAuthState())),
      animeApiProvider.overrideWithValue(FakeAnimeApi()),
      progressApiProvider.overrideWithValue(_SessionProgressApi()),
      mediaApiProvider.overrideWithValue(mediaApi),
      downloadProvider.overrideWith(_NoDownloadNotifier.new),
      playerEngineFactoryProvider.overrideWithValue(() {
        final player = _FakeNativePlayer();
        players.add(player);
        return (player: player, videoController: _FakeVideoController(player));
      }),
    ];
  }
}

PlayerConfig _configFor(int episodeId) => PlayerConfig(
  animeId: 1,
  episodeId: episodeId,
  serverUrl: 'http://127.0.0.1:8080',
  token: 'tok',
);

Future<void> _settle(WidgetTester tester, [int frames = 10]) async {
  for (var i = 0; i < frames; i++) {
    await tester.pump(const Duration(milliseconds: 10));
  }
}

/// 在 Ref 仍有效时完成生产 dispose（幂等），再释放 Provider。
///
/// 现有 dispose 在 await 后读取 state；若等 autoDispose/容器释放后再跑会抛
/// UnmountedRefException（与本轮修复无关的既有时序），测试里先显式收尾。
Future<void> _disposeWhileMounted(
  WidgetTester tester,
  PlayerNotifier notifier,
) async {
  var done = false;
  unawaited(notifier.dispose().whenComplete(() => done = true));
  for (var i = 0; i < 50 && !done; i++) {
    await tester.pump(const Duration(milliseconds: 10));
    if (!done) {
      // outbox 使用 SharedPreferences mock，需让真实事件循环推进一次。
      await tester.runAsync(() => Future<void>.delayed(Duration.zero));
    }
  }
  expect(done, isTrue);
}

class _ProviderSession {
  _ProviderSession(this.natives, this.container, this.subscription);

  final _Natives natives;
  final ProviderContainer container;
  final ProviderSubscription<PlayerState> subscription;

  _FakeNativePlayer get player => natives.players.single;
  PlayerState get state => subscription.read();
  PlayerNotifier get notifier =>
      container.read(playerProvider(_configFor(7)).notifier);

  Future<void> close(WidgetTester tester) async {
    // 先在 Ref 仍有效时走生产 dispose（幂等），再释放容器。
    await _disposeWhileMounted(tester, notifier);
    subscription.close();
    container.dispose();
    await _settle(tester);
  }
}

/// 运行生产 PlayerNotifier 初始化，直到首播 play 完成。
Future<_ProviderSession> _startProviderSession(WidgetTester tester) async {
  final natives = _Natives();
  final container = ProviderContainer(overrides: await natives.overrides());
  final subscription = container.listen<PlayerState>(
    playerProvider(_configFor(7)),
    (_, _) {},
  );
  await _settle(tester);
  final session = _ProviderSession(natives, container, subscription);
  expect(session.player.openCount, 1);
  expect(session.player.playCount, 1);
  expect(session.state.isPlaying, isTrue);
  return session;
}

class _ScreenHost extends StatefulWidget {
  const _ScreenHost({super.key});

  @override
  State<_ScreenHost> createState() => _ScreenHostState();
}

class _ScreenHostState extends State<_ScreenHost> {
  int episodeId = 1;

  void switchEpisode(int next) => setState(() => episodeId = next);

  @override
  Widget build(BuildContext context) {
    return PlayerScreen(
      key: const ValueKey('player-screen'),
      animeId: 1,
      episodeId: episodeId,
    );
  }
}

void _sessionWiringTests() {
  group('PlayerScreen 重绑接线', () {
    testWidgets('长按中同一 State 换集：无构建期 Provider 写入，旧句柄不影响新会话', (tester) async {
      final natives = _Natives();
      final hostKey = GlobalKey<_ScreenHostState>();
      await tester.pumpWidget(
        ProviderScope(
          overrides: await natives.overrides(),
          child: MaterialApp(home: _ScreenHost(key: hostKey)),
        ),
      );
      await _settle(tester);
      // 等初始控制层自动隐藏，长按落在手势层。
      await tester.pump(const Duration(seconds: 4));
      await _settle(tester);

      final container = ProviderScope.containerOf(
        tester.element(find.byType(PlayerScreen)),
      );
      final oldPlayer = natives.players.single;
      final oldNotifier = container.read(
        playerProvider(_configFor(1)).notifier,
      );
      // 保持旧会话存活，以便断言帧后结束的旧句柄只作用于旧会话。
      final keepOld = container.listen<PlayerState>(
        playerProvider(_configFor(1)),
        (_, _) {},
      );
      expect(container.read(playerProvider(_configFor(1))).isPlaying, isTrue);
      final screenState = tester.state(find.byType(PlayerScreen));

      final gesture = await tester.startGesture(
        tester.getCenter(find.byType(PlayerGestureSurface)),
      );
      await tester.pump(kLongPressTimeout + const Duration(milliseconds: 50));
      await tester.pump();
      expect(
        container.read(playerProvider(_configFor(1))).temporaryPlaybackRate,
        2,
      );
      expect(oldPlayer.rateWrites.last, 2);

      // 保持按住，更新同一 PlayerScreen 的播放配置进入 didUpdateWidget。
      hostKey.currentState!.switchEpisode(2);
      await tester.pump();
      expect(tester.takeException(), isNull);
      expect(
        identical(tester.state(find.byType(PlayerScreen)), screenState),
        isTrue,
      );

      await _settle(tester);
      expect(tester.takeException(), isNull);
      expect(natives.players, hasLength(2));
      // 捕获的旧句柄在帧后结束：旧会话恢复基准（或旧控制器已关闭时安全空操作）。
      expect(oldPlayer.rateWrites.last, 1);
      final newPlayer = natives.players.last;
      final newState = container.read(playerProvider(_configFor(2)));
      expect(newState.isPlaying, isTrue);

      // 旧手指继续右滑后松手：旧输入已失效，不能改变新会话速率。
      await gesture.moveBy(const Offset(240, 0));
      await tester.pump();
      await gesture.up();
      await _settle(tester);
      expect(tester.takeException(), isNull);
      final after = container.read(playerProvider(_configFor(2)));
      expect(after.temporaryPlaybackRate, isNull);
      expect(after.playbackRate, 1);
      expect(newPlayer.rateWrites.where((rate) => rate != 1), isEmpty);

      await _disposeWhileMounted(tester, oldNotifier);
      keepOld.close();
      await _disposeWhileMounted(
        tester,
        container.read(playerProvider(_configFor(2)).notifier),
      );
      await tester.pumpWidget(const SizedBox());
      await _settle(tester);
    });
  });

  group('媒体票据刷新守卫与续播', () {
    testWidgets('A 守卫超时后迟到 open 完成：play 0 次，无临时倍速', (tester) async {
      final session = await _startProviderSession(tester);
      final player = session.player;
      player.openGate = Completer<void>();
      player.errorController.add('stream broken');
      await _settle(tester);
      expect(player.openCount, 2);

      await tester.pump(const Duration(seconds: 16));
      await _settle(tester);
      expect(session.state.error, isNotNull);
      expect(session.state.temporaryPlaybackRate, isNull);

      player.openGate!.complete();
      await _settle(tester);
      expect(player.playCount - 1, 0);
      expect(session.natives.mediaApi.fetchCount, 2);
      expect(tester.takeException(), isNull);
      await session.close(tester);
    });

    testWidgets('B 守卫超时后迟到 seek 完成：不自动播放', (tester) async {
      final session = await _startProviderSession(tester);
      final player = session.player;
      player.setPosition(const Duration(seconds: 100));
      await _settle(tester);
      player.seekGate = Completer<void>();
      player.errorController.add('stream broken');
      await _settle(tester);
      expect(player.openCount, 2);
      expect(player.seeks, [const Duration(seconds: 100)]);

      await tester.pump(const Duration(seconds: 16));
      await _settle(tester);
      expect(session.state.error, isNotNull);

      player.seekGate!.complete();
      await _settle(tester);
      expect(player.playCount - 1, 0);
      expect(tester.takeException(), isNull);
      await session.close(tester);
    });

    testWidgets('C 正常刷新无超时无暂停：按原意图续播一次，无重复 open', (tester) async {
      final session = await _startProviderSession(tester);
      final player = session.player;
      player.errorController.add('stream broken');
      await _settle(tester);
      expect(player.openCount, 2);
      expect(player.playCount - 1, 1);
      expect(session.state.error, isNull);
      expect(session.natives.mediaApi.fetchCount, 2);
      expect(tester.takeException(), isNull);
      await session.close(tester);
    });

    testWidgets('C 刷新期间用户暂停：open 完成后不恢复', (tester) async {
      final session = await _startProviderSession(tester);
      final player = session.player;
      player.openGate = Completer<void>();
      player.errorController.add('stream broken');
      await _settle(tester);
      unawaited(session.notifier.pause());
      player.openGate!.complete();
      await _settle(tester);
      expect(player.playCount - 1, 0);
      expect(player.openCount, 2);
      expect(tester.takeException(), isNull);
      await session.close(tester);
    });

    testWidgets('C 刷新期间真正后台：open 完成后不恢复', (tester) async {
      final session = await _startProviderSession(tester);
      final player = session.player;
      player.openGate = Completer<void>();
      player.errorController.add('stream broken');
      await _settle(tester);
      unawaited(
        session.notifier.handleAppLifecycleState(AppLifecycleState.paused),
      );
      player.openGate!.complete();
      await _settle(tester);
      expect(player.playCount - 1, 0);
      expect(player.pauseCount, 1);
      expect(tester.takeException(), isNull);
      await session.close(tester);
    });

    testWidgets('C 刷新取票失败：旧引擎调速仍可用，不重开', (tester) async {
      final session = await _startProviderSession(tester);
      final player = session.player;
      session.natives.mediaApi.fetchError = const ApiException(9999, '服务器错误');
      player.errorController.add('stream broken');
      await _settle(tester);
      expect(player.openCount, 1);
      expect(session.state.error, isNotNull);

      unawaited(session.notifier.setPlaybackRate(1.5));
      await _settle(tester);
      expect(player.rateWrites.last, 1.5);
      expect(session.state.playbackRate, 1.5);
      expect(tester.takeException(), isNull);
      await session.close(tester);
    });

    testWidgets('C 取票阻塞超时：守卫期间不重入第二条刷新，迟到票据不 open', (tester) async {
      final session = await _startProviderSession(tester);
      final player = session.player;
      final mediaApi = session.natives.mediaApi;
      mediaApi.fetchGate = Completer<void>();
      player.errorController.add('stream broken');
      await _settle(tester);
      expect(mediaApi.fetchCount, 2);

      await tester.pump(const Duration(seconds: 16));
      await _settle(tester);
      expect(session.state.error, isNotNull);
      player.errorController.add('stream broken again');
      await _settle(tester);
      expect(mediaApi.fetchCount, 2);

      mediaApi.fetchGate!.complete();
      await _settle(tester);
      expect(player.openCount, 1);
      expect(player.playCount - 1, 0);

      unawaited(session.notifier.setPlaybackRate(1.5));
      await _settle(tester);
      expect(player.rateWrites.last, 1.5);
      expect(tester.takeException(), isNull);
      await session.close(tester);
    });
  });
}

void _seekByTests() {
  group('PlayerState.canSeekRelative', () {
    const ready = PlayerState(duration: Duration(minutes: 24));

    test('全部满足时允许', () {
      expect(ready.canSeekRelative, isTrue);
    });

    test('duration=0 拒绝', () {
      expect(const PlayerState().canSeekRelative, isFalse);
    });

    test('error 拒绝', () {
      expect(ready.copyWith(error: '播放失败').canSeekRelative, isFalse);
    });

    test('isLoading 拒绝', () {
      expect(ready.copyWith(isLoading: true).canSeekRelative, isFalse);
    });

    test('isRestoring 拒绝', () {
      expect(ready.copyWith(isRestoring: true).canSeekRelative, isFalse);
    });

    test('isCompleted 拒绝', () {
      expect(ready.copyWith(isCompleted: true).canSeekRelative, isFalse);
    });
  });

  group('PlayerNotifier.seekBy', () {
    const step = Duration(seconds: 80);
    const window = Duration(milliseconds: 1600);

    Future<_ProviderSession> startWithDuration(WidgetTester tester) async {
      final session = await _startProviderSession(tester);
      session.player.durationController.add(const Duration(minutes: 24));
      await _settle(tester, 2);
      expect(session.state.duration, const Duration(minutes: 24));
      expect(session.state.canSeekRelative, isTrue);
      return session;
    }

    Future<void> moveTo(
      WidgetTester tester,
      _ProviderSession session,
      Duration position,
    ) async {
      session.player.setPosition(position);
      await _settle(tester, 2);
      expect(session.state.position, position);
    }

    testWidgets('duration=24:00 时 seekBy(80s) 从当前位置下发', (tester) async {
      final session = await startWithDuration(tester);
      await moveTo(tester, session, const Duration(minutes: 10));

      final target = session.notifier.seekBy(step);
      expect(target, const Duration(minutes: 11, seconds: 20));
      expect(session.player.seeks.last, target);
      await _settle(tester, 2);
      expect(session.state.position, target);

      await tester.pump(window);
      await session.close(tester);
    });

    testWidgets('seek 未完成且 position 仍报旧值时连调累加，且不触碰 play/pause', (tester) async {
      final session = await startWithDuration(tester);
      await moveTo(tester, session, const Duration(minutes: 10));
      final playBefore = session.player.playCount;
      final pauseBefore = session.player.pauseCount;
      session.player.seekGate = Completer<void>();

      expect(
        session.notifier.seekBy(step),
        const Duration(minutes: 11, seconds: 20),
      );
      await tester.pump(const Duration(milliseconds: 150));
      session.player.setPosition(const Duration(minutes: 10));
      await tester.pump(const Duration(milliseconds: 150));
      expect(session.state.position, const Duration(minutes: 10));

      expect(
        session.notifier.seekBy(step),
        const Duration(minutes: 12, seconds: 40),
      );
      expect(
        session.player.seeks.last,
        const Duration(minutes: 12, seconds: 40),
      );

      session.player.seekGate!.complete();
      session.player.seekGate = null;
      await _settle(tester, 2);
      expect(session.player.playCount, playBefore);
      expect(session.player.pauseCount, pauseBefore);

      await tester.pump(window);
      await session.close(tester);
    });

    testWidgets('基线窗口到期后起点回到 state.position', (tester) async {
      final session = await startWithDuration(tester);
      await moveTo(tester, session, const Duration(minutes: 10));

      expect(
        session.notifier.seekBy(step),
        const Duration(minutes: 11, seconds: 20),
      );
      await _settle(tester, 2);
      await tester.pump(window);
      // 窗口到期后 position 流回报的新位置成为起点。
      await moveTo(tester, session, const Duration(minutes: 5));
      expect(
        session.notifier.seekBy(step),
        const Duration(minutes: 6, seconds: 20),
      );

      await tester.pump(window);
      await session.close(tester);
    });

    testWidgets('seek(15:00) 后立即 seekBy 从 15:00 起算', (tester) async {
      final session = await startWithDuration(tester);
      await moveTo(tester, session, const Duration(minutes: 10));

      unawaited(session.notifier.seek(const Duration(minutes: 15)));
      await tester.pump(const Duration(milliseconds: 100));
      session.player.setPosition(const Duration(minutes: 10));
      await tester.pump(const Duration(milliseconds: 100));
      expect(
        session.notifier.seekBy(step),
        const Duration(minutes: 16, seconds: 20),
      );

      await tester.pump(window);
      await session.close(tester);
    });

    testWidgets('末尾钳制到 duration-1s，已在上限时返回 null', (tester) async {
      final session = await startWithDuration(tester);
      await moveTo(tester, session, const Duration(minutes: 23, seconds: 30));

      expect(
        session.notifier.seekBy(step),
        const Duration(minutes: 23, seconds: 59),
      );
      await _settle(tester, 2);
      await tester.pump(window);

      await moveTo(tester, session, const Duration(minutes: 23, seconds: 59));
      final seeksBefore = session.player.seeks.length;
      expect(session.notifier.seekBy(step), isNull);
      expect(session.player.seeks.length, seeksBefore);

      await tester.pump(window);
      await session.close(tester);
    });

    testWidgets('duration 未知时返回 null 且不下发 seek', (tester) async {
      final session = await _startProviderSession(tester);
      expect(session.state.duration, Duration.zero);

      expect(session.notifier.seekBy(step), isNull);
      expect(session.player.seeks, isEmpty);

      await session.close(tester);
    });

    testWidgets('emit completed 后返回 null', (tester) async {
      final session = await startWithDuration(tester);
      await moveTo(tester, session, const Duration(minutes: 10));
      session.player.completedController.add(true);
      await _settle(tester, 2);
      expect(session.state.isCompleted, isTrue);

      final seeksBefore = session.player.seeks.length;
      expect(session.notifier.seekBy(step), isNull);
      expect(session.player.seeks.length, seeksBefore);

      await session.close(tester);
    });

    testWidgets('基线窗口内 dispose：无 pending timer', (tester) async {
      final session = await startWithDuration(tester);
      await moveTo(tester, session, const Duration(minutes: 10));

      expect(session.notifier.seekBy(step), isNotNull);
      await _settle(tester, 2);
      // 不推进 1.5 s 窗口直接销毁；测试框架会在结束时校验 pending timer。
      final notifier = session.notifier;
      await session.close(tester);
      expect(notifier.seekBy(step), isNull);
      expect(tester.takeException(), isNull);
    });
  });
}

/// 三集夹具：PlayerScreen 取中间集，保证上一集/下一集按钮都存在。
FakeAnimeApi _threeEpisodeApi() => FakeAnimeApi()
  ..episodes = const <Episode>[
    Episode(
      id: 1,
      animeId: 1,
      epNumber: 1,
      title: '01',
      filePath: '01.mkv',
      duration: 0,
    ),
    Episode(
      id: 2,
      animeId: 1,
      epNumber: 2,
      title: '02',
      filePath: '02.mkv',
      duration: 0,
    ),
    Episode(
      id: 3,
      animeId: 1,
      epNumber: 3,
      title: '03',
      filePath: '03.mkv',
      duration: 0,
    ),
  ];

class _MountedScreen {
  _MountedScreen(this.tester, this.natives);

  final WidgetTester tester;
  final _Natives natives;

  _FakeNativePlayer get player => natives.players.single;
  ProviderContainer get container =>
      ProviderScope.containerOf(tester.element(find.byType(PlayerScreen)));
  PlayerState get state => container.read(playerProvider(_configFor(2)));
  PlayerNotifier get notifier =>
      container.read(playerProvider(_configFor(2)).notifier);

  Future<void> close() async {
    await _disposeWhileMounted(tester, notifier);
    await tester.pumpWidget(const SizedBox());
    await _settle(tester);
  }
}

/// 以指定逻辑尺寸挂载真实 PlayerScreen（episodeId=2，前后集都存在）。
Future<_MountedScreen> _mountSkipScreen(
  WidgetTester tester, {
  Size size = const Size(360, 640),
  double textScale = 1,
}) async {
  tester.view.devicePixelRatio = 1;
  tester.view.physicalSize = size;
  addTearDown(tester.view.reset);
  final natives = _Natives();
  final overrides = await natives.overrides();
  // _Natives.overrides() 第 4 项为 animeApiProvider，替换为三集夹具。
  overrides[3] = animeApiProvider.overrideWithValue(_threeEpisodeApi());
  await tester.pumpWidget(
    ProviderScope(
      overrides: overrides,
      child: MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: TextScaler.linear(textScale)),
          child: child!,
        ),
        home: const PlayerScreen(animeId: 1, episodeId: 2),
      ),
    ),
  );
  await _settle(tester);
  final mounted = _MountedScreen(tester, natives);
  expect(mounted.player.openCount, 1);
  expect(mounted.state.isPlaying, isTrue);
  return mounted;
}

TextButton _skipButton(WidgetTester tester) => tester.widget<TextButton>(
  find.descendant(
    of: find.byKey(const Key('player-skip-forward')),
    matching: find.byType(TextButton),
  ),
);

void _skipSeekWiringTests() {
  group('跳过按钮接线', () {
    testWidgets('点击按钮：seeks 含 起点+80s，pause/play 计数不变', (tester) async {
      final screen = await _mountSkipScreen(tester);
      screen.player.durationController.add(const Duration(minutes: 24));
      screen.player.setPosition(const Duration(minutes: 10));
      await _settle(tester, 2);
      expect(screen.state.canSeekRelative, isTrue);
      expect(_skipButton(tester).onPressed, isNotNull);
      final pauses = screen.player.pauseCount;
      final plays = screen.player.playCount;

      await tester.tap(find.byKey(const Key('player-skip-forward')));
      await _settle(tester, 2);
      expect(tester.takeException(), isNull);
      expect(
        screen.player.seeks,
        contains(const Duration(minutes: 11, seconds: 20)),
      );
      expect(screen.player.pauseCount, pauses);
      expect(screen.player.playCount, plays);

      await tester.pump(const Duration(seconds: 2));
      await screen.close();
    });

    testWidgets('未 emit duration（加载态）时按钮 onPressed 为 null', (tester) async {
      final screen = await _mountSkipScreen(tester);
      expect(screen.state.duration, Duration.zero);
      expect(_skipButton(tester).onPressed, isNull);
      await screen.close();
    });

    testWidgets('尺寸矩阵 + textScaler 1.5：无 overflow，按钮不与居中簇相交', (tester) async {
      const sizes = [Size(360, 640), Size(800, 360), Size(320, 568)];
      for (final size in sizes) {
        final screen = await _mountSkipScreen(
          tester,
          size: size,
          textScale: 1.5,
        );
        screen.player.durationController.add(const Duration(minutes: 24));
        await _settle(tester, 2);
        expect(tester.takeException(), isNull, reason: '$size');
        expect(find.byTooltip('上一集'), findsOneWidget, reason: '$size');
        expect(find.byTooltip('下一集'), findsOneWidget, reason: '$size');
        final play = find.byTooltip(screen.state.isPlaying ? '暂停' : '播放');
        final cluster = tester
            .getRect(find.byTooltip('上一集'))
            .expandToInclude(tester.getRect(play))
            .expandToInclude(tester.getRect(find.byTooltip('下一集')));
        final button = tester.getRect(
          find.byKey(const Key('player-skip-forward')),
        );
        expect(button.width, lessThanOrEqualTo(64), reason: '$size');
        expect(button.height, greaterThanOrEqualTo(40), reason: '$size');
        expect(
          button.overlaps(cluster),
          isFalse,
          reason: '$size $button $cluster',
        );
        await screen.close();
      }
    });
  });

  group('拖动灵敏度接线', () {
    Future<Duration> dragOnce(
      WidgetTester tester, {
      required Size size,
      required Duration duration,
    }) async {
      final screen = await _mountSkipScreen(tester, size: size);
      screen.player.durationController.add(duration);
      const start = Duration(minutes: 5);
      screen.player.setPosition(start);
      await _settle(tester, 2);
      // 等控制层自动隐藏，拖动落在手势层。
      await tester.pump(const Duration(seconds: 4));
      await _settle(tester);
      final seeksBefore = screen.player.seeks.length;

      await tester.drag(
        find.byType(PlayerGestureSurface),
        const Offset(100, 0),
      );
      await _settle(tester);
      expect(tester.takeException(), isNull);
      expect(screen.player.seeks.length, greaterThan(seeksBefore));
      final delta = screen.player.seeks.last - start;
      expect(delta, greaterThan(Duration.zero));
      expect(delta, lessThanOrEqualTo(const Duration(seconds: 30)));

      await tester.pump(const Duration(seconds: 2));
      await screen.close();
      return delta;
    }

    testWidgets('手势层右拖 100px：增量在 (0, 30s]，与屏宽和时长无关', (tester) async {
      final narrow = await dragOnce(
        tester,
        size: const Size(360, 640),
        duration: const Duration(minutes: 24),
      );
      final wide = await dragOnce(
        tester,
        size: const Size(800, 360),
        duration: const Duration(minutes: 24),
      );
      final longer = await dragOnce(
        tester,
        size: const Size(360, 640),
        duration: const Duration(minutes: 48),
      );
      expect(wide, narrow);
      expect(longer, narrow);
    });
  });
}
