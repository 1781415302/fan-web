import 'dart:async';

import 'package:fan_web/models/playback_interaction.dart';
import 'package:fan_web/services/playback_interaction_controller.dart';
import 'package:fan_web/widgets/player_gesture_surface.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

const Duration _longPress = kLongPressTimeout;

/// 记录型原生端口：记录每次 rate/pause/play/seek，可用 Completer 阻塞 rate。
class _NativePorts {
  final List<double> rateWrites = <double>[];
  final List<Completer<void>> rateCompleters = <Completer<void>>[];
  bool blockRates = false;
  int pauseCount = 0;
  int playCount = 0;
  int seekCount = 0;
  int maxRateInFlight = 0;
  int _rateInFlight = 0;

  Future<void> writeRate(double rate) {
    rateWrites.add(rate);
    if (!blockRates) {
      return Future<void>.value();
    }
    _rateInFlight++;
    if (_rateInFlight > maxRateInFlight) {
      maxRateInFlight = _rateInFlight;
    }
    final completer = Completer<void>();
    rateCompleters.add(completer);
    return completer.future.whenComplete(() => _rateInFlight--);
  }

  Future<void> pauseAndReport() async {
    pauseCount++;
  }

  /// 依次完成在途 rate Future，直到没有新的派发。
  Future<void> drainRates(WidgetTester tester) async {
    var index = 0;
    while (index < rateCompleters.length) {
      final completer = rateCompleters[index++];
      if (!completer.isCompleted) {
        completer.complete();
      }
      await tester.pump();
    }
  }
}

/// 测试内最小 Riverpod Notifier：记录控制器 onRateChanged 发布的快照。
class _RateSnapshotNotifier extends Notifier<PlaybackRateSnapshot> {
  @override
  PlaybackRateSnapshot build() => const PlaybackRateSnapshot(
    baseRate: 1,
    temporaryRate: null,
    boostSessionId: null,
  );

  void publish(PlaybackRateSnapshot snapshot) {
    state = snapshot;
  }
}

final _rateSnapshotProvider =
    NotifierProvider<_RateSnapshotNotifier, PlaybackRateSnapshot>(
      _RateSnapshotNotifier.new,
    );

/// 一个播放会话：生产控制器 + 记录端口 + 模拟 pan/Slider 续播流程。
class _Session {
  _Session(
    this.ports, {
    void Function(PlaybackRateSnapshot snapshot)? onRateChanged,
    double initialBaseRate = 1,
  }) {
    controller = PlaybackInteractionController(
      writeRate: ports.writeRate,
      pauseAndReport: ports.pauseAndReport,
      onRateChanged: (snapshot) {
        snapshots.add(snapshot);
        onRateChanged?.call(snapshot);
      },
      onNotice: notices.add,
      initialBaseRate: initialBaseRate,
    );
  }

  final _NativePorts ports;
  late final PlaybackInteractionController controller;
  final List<PlaybackRateSnapshot> snapshots = <PlaybackRateSnapshot>[];
  final List<String> notices = <String>[];
  bool isPlaying = true;
  Completer<void>? seekCompleter;
  bool _panWasPlaying = false;
  int? _panCapturedRevision;

  Future<void> open() async {
    await controller.resumeAfterMediaOpen();
  }

  /// 模拟 Notifier.pause：首次 await 前同步 requestPause。
  void pause() {
    controller.requestPause();
    isPlaying = false;
    controller.onPlayingChanged(false);
  }

  /// 模拟 Notifier.play：requestPlay 允许时才调用原生 play。
  bool play({int? expectedIntentRevision}) {
    final allowed = controller.requestPlay(
      expectedIntentRevision: expectedIntentRevision,
    );
    if (allowed) {
      ports.playCount++;
      isPlaying = true;
      controller.onPlayingChanged(true);
    }
    return allowed;
  }

  /// pan/Slider 起点：本次 pause 后立即捕获意图版本。
  void beginSeekInteraction() {
    _panWasPlaying = isPlaying;
    if (isPlaying) {
      pause();
    }
    _panCapturedRevision = controller.intentRevision;
  }

  /// pan/Slider 完成：await 被 Completer 阻塞的 seek，再条件续播。
  Future<void> completeSeekInteraction() async {
    final wasPlaying = _panWasPlaying;
    final captured = _panCapturedRevision;
    _panCapturedRevision = null;
    ports.seekCount++;
    final completer = Completer<void>();
    seekCompleter = completer;
    await completer.future;
    if (wasPlaying && captured != null) {
      play(expectedIntentRevision: captured);
    }
  }

  TemporaryBoostSession? beginBoost() =>
      controller.beginBoost(eligible: isPlaying);
}

class _Harness extends ConsumerWidget {
  const _Harness({
    required this.session,
    this.enabled = true,
    this.withSlider = false,
  });

  final _Session session;
  final bool enabled;
  final bool withSlider;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final snapshot = ref.watch(_rateSnapshotProvider);
    return MaterialApp(
      home: Scaffold(
        body: Column(
          children: [
            Text(
              snapshot.temporaryRate == null
                  ? 'base ${snapshot.baseRate}'
                  : 'temp ${snapshot.temporaryRate}',
            ),
            Expanded(
              child: PlayerGestureSurface(
                key: const Key('surface'),
                sessionIdentity: session,
                enabled: enabled,
                boostEnabled: session.isPlaying,
                onBoostStart: session.beginBoost,
                onTap: () {},
                onDoubleTap: () {},
                onPanStart: (_) => session.beginSeekInteraction(),
                onPanUpdate: (_) {},
                onPanEnd: (_) {
                  unawaited(session.completeSeekInteraction());
                },
                onPanCancel: () {},
                onInteractionStart: () {},
                onInteractionEnd: () {},
              ),
            ),
            if (withSlider)
              Slider(
                key: const Key('slider'),
                value: 0.5,
                onChangeStart: (_) => session.beginSeekInteraction(),
                onChanged: (_) {},
                onChangeEnd: (_) {
                  unawaited(session.completeSeekInteraction());
                },
              ),
          ],
        ),
      ),
    );
  }
}

/// 无 Riverpod 订阅的简单挂载（A17）。
Widget _plain(_Session session) {
  return MaterialApp(
    home: Scaffold(
      body: PlayerGestureSurface(
        key: const Key('surface'),
        sessionIdentity: session,
        enabled: true,
        boostEnabled: session.isPlaying,
        onBoostStart: session.beginBoost,
        onTap: () {},
        onDoubleTap: () {},
        onPanStart: (_) => session.beginSeekInteraction(),
        onPanUpdate: (_) {},
        onPanEnd: (_) {
          unawaited(session.completeSeekInteraction());
        },
        onPanCancel: () {},
        onInteractionStart: () {},
        onInteractionEnd: () {},
      ),
    ),
  );
}

Offset _surfaceCenter(WidgetTester tester) =>
    tester.getCenter(find.byKey(const Key('surface')));

Future<TestGesture> _longPressAt(WidgetTester tester, Offset at) async {
  final gesture = await tester.startGesture(at);
  await tester.pump(_longPress + const Duration(milliseconds: 50));
  return gesture;
}

void main() {
  group('interaction integration', () {
    testWidgets('A17 长按+右滑+松手：目标序列 2→3→基准，无 play/pause/seek', (tester) async {
      final ports = _NativePorts();
      final session = _Session(ports, initialBaseRate: 1.25);
      await session.open();
      await tester.pumpWidget(_plain(session));
      ports.rateWrites.clear();

      final center = _surfaceCenter(tester);
      final gesture = await _longPressAt(tester, center);
      expect(session.controller.rate.temporaryRate, 2);
      await gesture.moveBy(const Offset(48, 0));
      await tester.pump();
      await gesture.moveBy(const Offset(48, 0));
      await tester.pump();
      expect(session.controller.rate.temporaryRate, 3);
      expect(session.controller.rate.baseRate, 1.25);
      await gesture.up();
      await tester.pump();

      expect(session.controller.rate.temporaryRate, isNull);
      expect(session.controller.rate.baseRate, 1.25);
      expect(ports.rateWrites.first, 2);
      expect(ports.rateWrites, contains(3.0));
      expect(ports.rateWrites.last, 1.25);
      expect(ports.playCount, 0);
      expect(ports.pauseCount, 0);
      expect(ports.seekCount, 0);
      expect(session.isPlaying, isTrue);
    });

    testWidgets('A17 长按成功后 PointerCancel 恢复基准，无 play/pause/seek', (
      tester,
    ) async {
      final ports = _NativePorts();
      final session = _Session(ports, initialBaseRate: 2);
      await session.open();
      await tester.pumpWidget(_plain(session));
      ports.rateWrites.clear();

      final gesture = await _longPressAt(tester, _surfaceCenter(tester));
      await gesture.moveBy(const Offset(-48, 0));
      await tester.pump();
      expect(session.controller.rate.temporaryRate, 1.5);
      await gesture.cancel();
      await tester.pump();

      expect(session.controller.rate.temporaryRate, isNull);
      expect(ports.rateWrites, <double>[2, 1.5, 2]);
      expect(ports.playCount, 0);
      expect(ports.pauseCount, 0);
      expect(ports.seekCount, 0);
    });

    testWidgets('A17 长按中 inactive：恢复基准、pause 端口 0 次，余下移动无效', (tester) async {
      final ports = _NativePorts();
      final session = _Session(ports);
      await session.open();
      await tester.pumpWidget(_plain(session));
      ports.rateWrites.clear();

      final gesture = await _longPressAt(tester, _surfaceCenter(tester));
      expect(session.controller.rate.temporaryRate, 2);
      await session.controller.handleLifecycleState(AppLifecycleState.inactive);
      await tester.pump();
      expect(session.controller.rate.temporaryRate, isNull);

      await gesture.moveBy(const Offset(96, 0));
      await tester.pump();
      await gesture.up();
      await tester.pump();
      await session.controller.handleLifecycleState(AppLifecycleState.resumed);
      await tester.pump();

      expect(ports.rateWrites, <double>[2, 1]);
      expect(ports.pauseCount, 0);
      expect(ports.playCount, 0);
      expect(ports.seekCount, 0);
      expect(session.controller.mayPlay, isTrue);
    });

    testWidgets('A17 慢 Future：松手早于 3x 完成，最终仍写回基准且最多一个在途', (tester) async {
      final ports = _NativePorts()..blockRates = true;
      final session = _Session(ports, initialBaseRate: 1.25);
      await session.open();
      await tester.pumpWidget(_plain(session));
      await ports.drainRates(tester);
      ports.rateWrites.clear();
      ports.rateCompleters.clear();

      final gesture = await _longPressAt(tester, _surfaceCenter(tester));
      await gesture.moveBy(const Offset(48, 0));
      await tester.pump();
      await gesture.moveBy(const Offset(48, 0));
      await tester.pump();
      await gesture.up();
      await tester.pump();
      expect(session.controller.rate.temporaryRate, isNull);
      expect(ports.rateWrites, <double>[2]);

      await ports.drainRates(tester);
      expect(ports.rateWrites.last, 1.25);
      expect(ports.maxRateInFlight, 1);
      expect(ports.playCount, 0);
      expect(ports.pauseCount, 0);
    });

    testWidgets('A18 快速切档后立即失焦：慢 Future 结束不污染，订阅无异常', (tester) async {
      final ports = _NativePorts()..blockRates = true;
      late final _Session session;
      final container = ProviderContainer();
      addTearDown(container.dispose);
      session = _Session(
        ports,
        onRateChanged: container.read(_rateSnapshotProvider.notifier).publish,
      );
      await session.open();
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: _Harness(session: session),
        ),
      );
      await ports.drainRates(tester);

      final gesture = await _longPressAt(tester, _surfaceCenter(tester));
      await gesture.moveBy(const Offset(48, 0));
      await tester.pump();
      await gesture.moveBy(const Offset(48, 0));
      await tester.pump();
      expect(find.text('temp 3.0'), findsOneWidget);
      await session.controller.handleLifecycleState(AppLifecycleState.inactive);
      await tester.pump();
      expect(find.text('base 1.0'), findsOneWidget);

      await ports.drainRates(tester);
      await gesture.up();
      await tester.pump();
      expect(ports.rateWrites.last, 1);
      expect(container.read(_rateSnapshotProvider).temporaryRate, isNull);
      expect(find.text('base 1.0'), findsOneWidget);
      expect(ports.pauseCount, 0);
      expect(tester.takeException(), isNull);
    });

    testWidgets('A18 长按中切会话：旧句柄延后 end 只影响旧会话，新会话不变', (tester) async {
      final portsA = _NativePorts()..blockRates = true;
      final portsB = _NativePorts();
      final container = ProviderContainer();
      addTearDown(container.dispose);
      final publish = container.read(_rateSnapshotProvider.notifier).publish;
      final sessionA = _Session(portsA, onRateChanged: publish);
      await sessionA.open();
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: _Harness(session: sessionA),
        ),
      );
      await portsA.drainRates(tester);

      final gesture = await _longPressAt(tester, _surfaceCenter(tester));
      await gesture.moveBy(const Offset(96, 0));
      await tester.pump();
      expect(sessionA.controller.rate.temporaryRate, 3);

      // 换集：旧控制器销毁，新会话 B 接入同一 surface。
      final sessionB = _Session(portsB, initialBaseRate: 1.5);
      await sessionB.open();
      final bSnapshotsBefore = sessionB.snapshots.length;
      final bWritesBefore = List<double>.of(portsB.rateWrites);
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: _Harness(session: sessionB),
        ),
      );
      await tester.pump();
      sessionA.controller.dispose();

      // 旧手指继续移动/松手，旧慢 Future 迟到完成。
      await gesture.moveBy(const Offset(-96, 0));
      await tester.pump();
      await gesture.up();
      await tester.pump();
      await portsA.drainRates(tester);

      expect(sessionB.snapshots.length, bSnapshotsBefore);
      expect(portsB.rateWrites, bWritesBefore);
      expect(sessionB.controller.rate.temporaryRate, isNull);
      expect(sessionB.controller.rate.baseRate, 1.5);
      expect(portsB.pauseCount, 0);
      expect(portsB.playCount, 0);
      expect(tester.takeException(), isNull);
    });

    testWidgets('A18 长按中禁用与卸载 widget：延后 end 安全，无同步 Provider 写入错误', (
      tester,
    ) async {
      final ports = _NativePorts()..blockRates = true;
      final container = ProviderContainer();
      addTearDown(container.dispose);
      final session = _Session(
        ports,
        onRateChanged: container.read(_rateSnapshotProvider.notifier).publish,
      );
      await session.open();
      Widget build({required bool enabled, bool mounted = true}) {
        return UncontrolledProviderScope(
          container: container,
          child: mounted
              ? _Harness(session: session, enabled: enabled)
              : const MaterialApp(home: SizedBox()),
        );
      }

      await tester.pumpWidget(build(enabled: true));
      await ports.drainRates(tester);

      // 禁用：结束 boost。
      var gesture = await _longPressAt(tester, _surfaceCenter(tester));
      await gesture.moveBy(const Offset(48, 0));
      await tester.pump();
      expect(session.controller.rate.temporaryRate, 2.5);
      await tester.pumpWidget(build(enabled: false));
      await tester.pump();
      expect(tester.takeException(), isNull);
      expect(session.controller.rate.temporaryRate, isNull);
      await gesture.up();
      await tester.pump();

      // 重新启用后卸载。
      await tester.pumpWidget(build(enabled: true));
      gesture = await _longPressAt(tester, _surfaceCenter(tester));
      await gesture.moveBy(const Offset(96, 0));
      await tester.pump();
      expect(session.controller.rate.temporaryRate, 3);
      await tester.pumpWidget(build(enabled: true, mounted: false));
      await tester.pump();
      expect(tester.takeException(), isNull);
      expect(session.controller.rate.temporaryRate, isNull);
      expect(container.read(_rateSnapshotProvider).temporaryRate, isNull);
      await gesture.up();
      await tester.pump();

      await ports.drainRates(tester);
      expect(ports.rateWrites.last, 1);
      expect(ports.maxRateInFlight, 1);
      expect(ports.pauseCount, 0);
      expect(ports.playCount, 0);
      expect(tester.takeException(), isNull);
    });

    testWidgets('A20 pan：seek 阻塞期间 hidden→resumed，旧条件续播不能 play', (
      tester,
    ) async {
      final ports = _NativePorts();
      final session = _Session(ports);
      await session.open();
      await tester.pumpWidget(_plain(session));

      final gesture = await tester.startGesture(_surfaceCenter(tester));
      await gesture.moveBy(const Offset(40, 0));
      await tester.pump();
      await gesture.moveBy(const Offset(40, 0));
      await tester.pump();
      await gesture.up();
      await tester.pump();
      expect(ports.seekCount, 1);
      expect(session.isPlaying, isFalse);

      await session.controller.handleLifecycleState(AppLifecycleState.hidden);
      await session.controller.handleLifecycleState(AppLifecycleState.resumed);
      session.seekCompleter!.complete();
      await tester.pump();

      expect(ports.playCount, 0);
      expect(session.isPlaying, isFalse);
      expect(ports.pauseCount, 1);
      // 排空手势识别器残留计时器。
      await tester.pump(_longPress);
    });

    testWidgets('A20 Slider：seek 阻塞期间新用户 pause，旧条件续播不能 play', (tester) async {
      final ports = _NativePorts();
      final container = ProviderContainer();
      addTearDown(container.dispose);
      final session = _Session(
        ports,
        onRateChanged: container.read(_rateSnapshotProvider.notifier).publish,
      );
      await session.open();
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: _Harness(session: session, withSlider: true),
        ),
      );

      await tester.drag(find.byKey(const Key('slider')), const Offset(60, 0));
      await tester.pump();
      expect(ports.seekCount, 1);

      // 用户在 seek 完成前再次暂停（新的暂停意图）。
      session.pause();
      session.seekCompleter!.complete();
      await tester.pump();

      expect(ports.playCount, 0);
      expect(session.isPlaying, isFalse);
      expect(tester.takeException(), isNull);
    });

    testWidgets('A20 未被中断的 pan/Slider seek 正常续播一次', (tester) async {
      final ports = _NativePorts();
      final session = _Session(ports);
      await session.open();
      await tester.pumpWidget(
        ProviderScope(child: _Harness(session: session, withSlider: true)),
      );

      final gesture = await tester.startGesture(_surfaceCenter(tester));
      await gesture.moveBy(const Offset(40, 0));
      await tester.pump();
      await gesture.moveBy(const Offset(40, 0));
      await tester.pump();
      await gesture.up();
      await tester.pump();
      session.seekCompleter!.complete();
      await tester.pump();
      expect(ports.playCount, 1);
      expect(session.isPlaying, isTrue);

      await tester.drag(find.byKey(const Key('slider')), const Offset(60, 0));
      await tester.pump();
      session.seekCompleter!.complete();
      await tester.pump();
      expect(ports.seekCount, 2);
      expect(ports.playCount, 2);
      expect(session.isPlaying, isTrue);
      expect(ports.pauseCount, 0);
      expect(tester.takeException(), isNull);
      // 排空手势识别器残留计时器。
      await tester.pump(_longPress);
    });
  });
}
