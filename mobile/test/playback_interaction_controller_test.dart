import 'dart:async';

import 'package:flutter/widgets.dart' show AppLifecycleState;
import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/models/playback_interaction.dart';
import 'package:fan_web/services/playback_interaction_controller.dart';

/// 记录型假原生端口：每次 writeRate 返回一个可手动完成的 Completer。
class _FakePorts {
  _FakePorts({this.autoComplete = true});

  bool autoComplete;
  final List<double> writes = <double>[];
  final List<Completer<void>> pendingWrites = <Completer<void>>[];
  final List<PlaybackRateSnapshot> snapshots = <PlaybackRateSnapshot>[];
  final List<String> notices = <String>[];
  int pauseCalls = 0;
  int inFlight = 0;
  int maxInFlight = 0;

  Future<void> writeRate(double rate) {
    writes.add(rate);
    inFlight++;
    if (inFlight > maxInFlight) {
      maxInFlight = inFlight;
    }
    final completer = Completer<void>();
    pendingWrites.add(completer);
    final future = completer.future.whenComplete(() => inFlight--);
    if (autoComplete) {
      completer.complete();
    }
    return future;
  }

  Future<void> pauseAndReport() async {
    pauseCalls++;
  }

  /// 完成最早一个尚未完成的原生写入。
  Future<void> completeNext({Object? error}) async {
    final completer = pendingWrites.firstWhere((c) => !c.isCompleted);
    if (error != null) {
      completer.completeError(error);
    } else {
      completer.complete();
    }
    await _pumpMicrotasks();
  }

  PlaybackInteractionController create({
    double initialBaseRate = 1,
    AppLifecycleState initialLifecycleState = AppLifecycleState.resumed,
  }) {
    return PlaybackInteractionController(
      writeRate: writeRate,
      pauseAndReport: pauseAndReport,
      onRateChanged: snapshots.add,
      onNotice: notices.add,
      initialBaseRate: initialBaseRate,
      initialLifecycleState: initialLifecycleState,
    );
  }
}

Future<void> _pumpMicrotasks() async {
  for (var i = 0; i < 10; i++) {
    await Future<void>.value();
  }
}

/// 创建并完成首次 media open 的控制器。
Future<PlaybackInteractionController> _openedController(
  _FakePorts ports, {
  double initialBaseRate = 1,
}) async {
  final controller = ports.create(initialBaseRate: initialBaseRate);
  await controller.resumeAfterMediaOpen();
  await controller.flushRates();
  await _pumpMicrotasks();
  ports.writes.clear();
  ports.snapshots.clear();
  return controller;
}

void main() {
  group('rate session', () {
    test('A02 base 1.25 → 2 → 3 → release back to 1.25', () async {
      final ports = _FakePorts();
      final controller = await _openedController(ports, initialBaseRate: 1.25);

      final session = controller.beginBoost(eligible: true)!;
      expect(controller.rate.temporaryRate, 2);
      expect(controller.rate.baseRate, 1.25);
      await _pumpMicrotasks();
      session.updateDisplacement(48);
      await _pumpMicrotasks();
      session.updateDisplacement(96);
      await _pumpMicrotasks();
      expect(controller.rate.effectiveRate, 3);
      session.end();
      await controller.flushRates();

      expect(controller.rate.temporaryRate, isNull);
      expect(controller.rate.boostSessionId, isNull);
      expect(controller.rate.baseRate, 1.25);
      expect(ports.writes, <double>[2, 2.5, 3, 1.25]);
      expect(ports.pauseCalls, 0);
    });

    test('A02 base 2 drag left to 1.5 and release back to 2', () async {
      final ports = _FakePorts();
      final controller = await _openedController(ports, initialBaseRate: 2);

      final session = controller.beginBoost(eligible: true)!;
      await _pumpMicrotasks();
      session.updateDisplacement(-48);
      expect(controller.rate.temporaryRate, 1.5);
      await _pumpMicrotasks();
      session.end();
      await controller.flushRates();

      expect(ports.writes, <double>[2, 1.5, 2]);
      expect(controller.rate.effectiveRate, 2);
    });

    test(
      'A02 base 0.5 releases back to 0.5 and each boost starts at 2',
      () async {
        final ports = _FakePorts();
        final controller = await _openedController(ports, initialBaseRate: 0.5);

        final first = controller.beginBoost(eligible: true)!;
        first.updateDisplacement(96);
        first.end();
        final second = controller.beginBoost(eligible: true)!;
        expect(controller.rate.temporaryRate, 2);
        second.end();
        await controller.flushRates();

        expect(controller.rate.effectiveRate, 0.5);
        expect(ports.writes.last, 0.5);
      },
    );

    test('A02 update only publishes when selected step changes', () async {
      final ports = _FakePorts();
      final controller = await _openedController(ports);

      final session = controller.beginBoost(eligible: true)!;
      final published = ports.snapshots.length;
      session.updateDisplacement(10);
      session.updateDisplacement(-20);
      expect(ports.snapshots.length, published);
      await controller.flushRates();
      expect(ports.writes, <double>[2]);
    });

    test('A03 stale handle, repeated end and repeated begin', () async {
      final ports = _FakePorts();
      final controller = await _openedController(ports, initialBaseRate: 1.5);

      final session = controller.beginBoost(eligible: true)!;
      expect(controller.beginBoost(eligible: true), isNull);
      expect(controller.rate.baseRate, 1.5);
      session.end();
      session.end();
      final writesAfterEnd = ports.writes.length;
      session.updateDisplacement(96);
      expect(controller.rate.temporaryRate, isNull);

      final next = controller.beginBoost(eligible: true)!;
      expect(next.id, isNot(session.id));
      session.end();
      expect(controller.rate.temporaryRate, 2);
      next.end();
      await controller.flushRates();
      expect(ports.writes.length, greaterThan(writesAfterEnd));
      expect(ports.writes.last, 1.5);
      expect(controller.rate.effectiveRate, 1.5);
    });

    test('A03 ineligible or not opened cannot begin boost', () async {
      final ports = _FakePorts();
      final unopened = ports.create();
      expect(unopened.beginBoost(eligible: true), isNull);
      final controller = await _openedController(ports);
      expect(controller.beginBoost(eligible: false), isNull);
    });

    test(
      'A03 normal menu change during boost wins over finger release',
      () async {
        final ports = _FakePorts();
        final controller = await _openedController(ports);

        final session = controller.beginBoost(eligible: true)!;
        controller.setBaseRate(1.25);
        expect(controller.rate.temporaryRate, isNull);
        expect(controller.rate.baseRate, 1.25);
        session.end();
        session.updateDisplacement(96);
        await controller.flushRates();

        expect(controller.rate.effectiveRate, 1.25);
        expect(ports.writes.last, 1.25);
      },
    );

    test(
      'A03 same numeric id from different controllers is isolated',
      () async {
        final portsA = _FakePorts();
        final portsB = _FakePorts();
        final a = await _openedController(portsA);
        final b = await _openedController(portsB);

        final sessionA = a.beginBoost(eligible: true)!;
        final sessionB = b.beginBoost(eligible: true)!;
        expect(sessionA.id, sessionB.id);
        sessionA.end();
        sessionA.updateDisplacement(96);

        expect(b.rate.temporaryRate, 2);
        expect(a.rate.temporaryRate, isNull);
        sessionB.end();
      },
    );

    test(
      'A03 snapshot keeps temporary/id pairing and constructor is silent',
      () {
        final ports = _FakePorts();
        final controller = ports.create(initialBaseRate: 3);
        expect(ports.snapshots, isEmpty);
        expect(controller.rate.baseRate, 1);
        expect(controller.rate.temporaryRate, isNull);
        expect(controller.rate.boostSessionId, isNull);
      },
    );
  });

  group('rate queue', () {
    test(
      'A04 many moves keep at most one write in flight, latest target only',
      () async {
        final ports = _FakePorts(autoComplete: false);
        final controller = ports.create();
        await controller.resumeAfterMediaOpen();
        await ports.completeNext();
        ports.writes.clear();

        final session = controller.beginBoost(eligible: true)!;
        session.updateDisplacement(48);
        session.updateDisplacement(96);
        session.updateDisplacement(48);
        session.updateDisplacement(96);
        expect(ports.inFlight, 1);
        expect(ports.writes, <double>[2]);

        await ports.completeNext();
        expect(ports.writes, <double>[2, 3]);
        await ports.completeNext();
        expect(ports.maxInFlight, 1);
        expect(ports.inFlight, 0);
        session.end();
      },
    );

    test(
      'A04 release before old high-rate future completes ends at base',
      () async {
        final ports = _FakePorts(autoComplete: false);
        final controller = ports.create(initialBaseRate: 1.25);
        await controller.resumeAfterMediaOpen();
        await ports.completeNext();
        ports.writes.clear();

        final session = controller.beginBoost(eligible: true)!;
        session.updateDisplacement(96);
        session.end();
        expect(controller.rate.temporaryRate, isNull);
        final flushed = controller.flushRates();

        await ports.completeNext();
        await ports.completeNext();
        await flushed;
        expect(ports.writes, <double>[2, 1.25]);
        expect(controller.rate.effectiveRate, 1.25);
        expect(ports.maxInFlight, 1);
      },
    );

    test(
      'A05 stale request failure does not roll back newer request',
      () async {
        final ports = _FakePorts(autoComplete: false);
        final controller = ports.create();
        await controller.resumeAfterMediaOpen();
        await ports.completeNext();

        final session = controller.beginBoost(eligible: true)!;
        session.updateDisplacement(96);
        await ports.completeNext(error: StateError('old 2x failed'));
        expect(controller.rate.temporaryRate, 3);
        expect(ports.notices, isEmpty);
        await ports.completeNext();
        session.end();
        await ports.completeNext();
        expect(ports.writes.last, 1);
      },
    );

    test(
      'A05 current temporary failure ends session and restores base once',
      () async {
        final ports = _FakePorts(autoComplete: false);
        final controller = ports.create(initialBaseRate: 1.5);
        await controller.resumeAfterMediaOpen();
        await ports.completeNext();
        ports.writes.clear();

        final session = controller.beginBoost(eligible: true)!;
        await ports.completeNext(error: StateError('2x failed'));
        expect(controller.rate.temporaryRate, isNull);
        expect(controller.rate.baseRate, 1.5);
        expect(ports.notices, <String>['倍速切换失败']);
        expect(ports.writes, <double>[2, 1.5]);
        await ports.completeNext();
        session.updateDisplacement(96);
        expect(ports.writes, <double>[2, 1.5]);
      },
    );

    test(
      'A05 current base failure rolls back to last successful base',
      () async {
        final ports = _FakePorts(autoComplete: false);
        final controller = ports.create();
        await controller.resumeAfterMediaOpen();
        await ports.completeNext();

        // 临时成功写入不能成为最后成功基准。
        final session = controller.beginBoost(eligible: true)!;
        await ports.completeNext();
        session.end();
        await ports.completeNext();

        controller.setBaseRate(1.5);
        expect(controller.rate.baseRate, 1.5);
        await ports.completeNext(error: StateError('1.5 failed'));
        expect(controller.rate.baseRate, 1);
        expect(ports.notices, <String>['倍速切换失败']);
        expect(ports.writes.last, 1);
        await ports.completeNext();
        expect(controller.rate.baseRate, 1);
      },
    );

    test('A05 recovery failing again does not loop or notify twice', () async {
      final ports = _FakePorts(autoComplete: false);
      final controller = ports.create();
      await controller.resumeAfterMediaOpen();
      await ports.completeNext();
      ports.writes.clear();

      controller.setBaseRate(2);
      await ports.completeNext(error: StateError('2 failed'));
      await ports.completeNext(error: StateError('recovery failed'));
      await _pumpMicrotasks();

      expect(ports.writes, <double>[2, 1]);
      expect(ports.notices.length, 1);
      expect(ports.inFlight, 0);
      await controller.flushRates();
    });

    test('A05 synchronous throw from writeRate is captured', () async {
      final notices = <String>[];
      var calls = 0;
      final controller = PlaybackInteractionController(
        writeRate: (rate) {
          calls++;
          throw StateError('sync throw');
        },
        pauseAndReport: () async {},
        onRateChanged: (_) {},
        onNotice: notices.add,
      );
      await controller.resumeAfterMediaOpen();
      await controller.flushRates();
      await _pumpMicrotasks();
      expect(calls, 2);
      expect(notices.length, 1);
    });

    test('A06 no native write before first open; resume forces base', () async {
      final ports = _FakePorts();
      final controller = ports.create(initialBaseRate: 1);
      controller.setBaseRate(1.25);
      await controller.flushRates();
      expect(ports.writes, isEmpty);
      expect(controller.rate.baseRate, 1.25);

      await controller.resumeAfterMediaOpen();
      await controller.flushRates();
      expect(ports.writes, <double>[1.25]);

      await controller.suspendForMediaOpen();
      await controller.resumeAfterMediaOpen();
      await controller.flushRates();
      expect(ports.writes, <double>[1.25, 1.25]);
    });

    test(
      'A06 suspend waits dispatched write and dispatches nothing new',
      () async {
        final ports = _FakePorts(autoComplete: false);
        final controller = ports.create();
        await controller.resumeAfterMediaOpen();
        await ports.completeNext();
        ports.writes.clear();

        final session = controller.beginBoost(eligible: true)!;
        session.updateDisplacement(96);
        var suspended = false;
        final suspendFuture = controller.suspendForMediaOpen().then(
          (_) => suspended = true,
        );
        await _pumpMicrotasks();
        expect(suspended, isFalse);
        expect(controller.rate.temporaryRate, isNull);

        controller.setBaseRate(1.5);
        await ports.completeNext();
        await suspendFuture;
        expect(suspended, isTrue);
        expect(ports.writes, <double>[2]);
        expect(controller.beginBoost(eligible: true), isNull);
        await controller.flushRates();

        controller.mediaOpenFailed();
        expect(ports.writes, <double>[2]);
        await controller.resumeAfterMediaOpen();
        expect(ports.writes, <double>[2, 1.5]);
        await ports.completeNext();
      },
    );

    test(
      'A06 dispose returns immediately and late results are inert',
      () async {
        final ports = _FakePorts(autoComplete: false);
        final controller = ports.create();
        await controller.resumeAfterMediaOpen();
        await ports.completeNext();

        final session = controller.beginBoost(eligible: true)!;
        session.updateDisplacement(96);
        final flushed = controller.flushRates();
        final snapshotsBefore = ports.snapshots.length;
        controller.dispose();
        await flushed;

        await ports.completeNext(error: StateError('late'));
        session.end();
        controller.setBaseRate(2);
        controller.requestPause();
        await controller.handleLifecycleState(AppLifecycleState.paused);
        await _pumpMicrotasks();
        expect(ports.writes.length, 2);
        expect(ports.snapshots.length, snapshotsBefore);
        expect(ports.notices, isEmpty);
        expect(ports.pauseCalls, 0);
        expect(controller.mayPlay, isFalse);
      },
    );
  });

  group('visibility policy', () {
    test('A07 resumed → inactive → resumed never pauses', () async {
      final ports = _FakePorts();
      final controller = await _openedController(ports, initialBaseRate: 1.25);
      final revision = controller.intentRevision;

      final session = controller.beginBoost(eligible: true)!;
      await controller.handleLifecycleState(AppLifecycleState.inactive);
      expect(controller.rate.temporaryRate, isNull);
      expect(controller.beginBoost(eligible: true), isNull);
      session.updateDisplacement(96);
      await controller.handleLifecycleState(AppLifecycleState.resumed);
      await controller.flushRates();

      expect(ports.pauseCalls, 0);
      expect(controller.mayPlay, isTrue);
      expect(controller.intentRevision, revision);
      expect(controller.rate.temporaryRate, isNull);
      expect(ports.writes.last, 1.25);
    });

    test(
      'A08 hidden → paused → detached pauses once; resumed does not play',
      () async {
        final ports = _FakePorts();
        final controller = await _openedController(ports);

        await controller.handleLifecycleState(AppLifecycleState.inactive);
        await controller.handleLifecycleState(AppLifecycleState.hidden);
        await controller.handleLifecycleState(AppLifecycleState.paused);
        await controller.handleLifecycleState(AppLifecycleState.detached);
        expect(ports.pauseCalls, 1);
        expect(controller.mayPlay, isFalse);

        await controller.handleLifecycleState(AppLifecycleState.inactive);
        expect(controller.requestPlay(), isFalse);
        await controller.handleLifecycleState(AppLifecycleState.resumed);
        expect(controller.mayPlay, isFalse);

        await controller.handleLifecycleState(AppLifecycleState.hidden);
        expect(ports.pauseCalls, 2);
      },
    );

    test('A08 manual pause is not auto resumed after focus returns', () async {
      final ports = _FakePorts();
      final controller = await _openedController(ports);
      controller.requestPause();
      await controller.handleLifecycleState(AppLifecycleState.inactive);
      expect(controller.beginBoost(eligible: false), isNull);
      await controller.handleLifecycleState(AppLifecycleState.resumed);
      expect(controller.mayPlay, isFalse);
      expect(ports.pauseCalls, 0);
    });

    test(
      'A08 background sets pause intent synchronously before async pause',
      () async {
        final ports = _FakePorts();
        final controller = await _openedController(ports);
        final revision = controller.intentRevision;
        final future = controller.handleLifecycleState(
          AppLifecycleState.hidden,
        );
        expect(controller.mayPlay, isFalse);
        expect(controller.intentRevision, revision + 1);
        await future;
      },
    );

    test('A08 pause port errors are captured', () async {
      final controller = PlaybackInteractionController(
        writeRate: (_) async {},
        pauseAndReport: () async => throw StateError('pause failed'),
        onRateChanged: (_) {},
        onNotice: (_) {},
      );
      await controller.handleLifecycleState(AppLifecycleState.paused);
      expect(controller.mayPlay, isFalse);
    });

    test(
      'A09 initial hidden/detached forbids autoplay and background play',
      () async {
        for (final state in <AppLifecycleState>[
          AppLifecycleState.hidden,
          AppLifecycleState.detached,
          AppLifecycleState.paused,
        ]) {
          final ports = _FakePorts();
          final controller = ports.create(initialLifecycleState: state);
          await controller.resumeAfterMediaOpen();
          expect(controller.mayPlay, isFalse);
          expect(
            controller.shouldPlayAfterMediaOpen(
              wasPlaying: true,
              capturedIntentRevision: controller.intentRevision,
            ),
            isFalse,
          );
          expect(controller.requestPlay(), isFalse);
          expect(controller.mayPlay, isFalse);
        }
      },
    );

    test('A09 initial resumed/inactive allows initial autoplay', () async {
      for (final state in <AppLifecycleState>[
        AppLifecycleState.resumed,
        AppLifecycleState.inactive,
      ]) {
        final ports = _FakePorts();
        final controller = ports.create(initialLifecycleState: state);
        final revision = controller.intentRevision;
        await controller.resumeAfterMediaOpen();
        expect(
          controller.shouldPlayAfterMediaOpen(
            wasPlaying: true,
            capturedIntentRevision: revision,
          ),
          isTrue,
        );
      }
    });

    test('A09 events after dispose have no side effects', () async {
      final ports = _FakePorts();
      final controller = await _openedController(ports);
      controller.dispose();
      await controller.handleLifecycleState(AppLifecycleState.hidden);
      expect(controller.requestPlay(), isFalse);
      expect(controller.beginBoost(eligible: true), isNull);
      controller.onPlayingChanged(false);
      controller.cancelBoost();
      controller.mediaOpenFailed();
      await controller.suspendForMediaOpen();
      await controller.resumeAfterMediaOpen();
      await controller.flushRates();
      expect(ports.pauseCalls, 0);
      expect(ports.writes, isEmpty);
      expect(ports.snapshots, isEmpty);
    });
  });

  group('open intent', () {
    test('A10 user pause during open blocks old captured intent', () async {
      final ports = _FakePorts();
      final controller = await _openedController(ports);
      final captured = controller.intentRevision;
      await controller.suspendForMediaOpen();
      controller.requestPause();
      await controller.resumeAfterMediaOpen();
      expect(
        controller.shouldPlayAfterMediaOpen(
          wasPlaying: true,
          capturedIntentRevision: captured,
        ),
        isFalse,
      );
    });

    test('A10 hidden during open blocks old captured intent', () async {
      final ports = _FakePorts();
      final controller = await _openedController(ports);
      final captured = controller.intentRevision;
      await controller.suspendForMediaOpen();
      await controller.handleLifecycleState(AppLifecycleState.hidden);
      await controller.handleLifecycleState(AppLifecycleState.resumed);
      await controller.resumeAfterMediaOpen();
      expect(
        controller.shouldPlayAfterMediaOpen(
          wasPlaying: true,
          capturedIntentRevision: captured,
        ),
        isFalse,
      );
    });

    test(
      'A10 stale expectedIntentRevision cannot create a new request',
      () async {
        final ports = _FakePorts();
        final controller = await _openedController(ports);
        final captured = controller.intentRevision;
        controller.requestPause();
        final afterPause = controller.intentRevision;
        expect(
          controller.requestPlay(expectedIntentRevision: captured),
          isFalse,
        );
        expect(controller.intentRevision, afterPause);
        expect(controller.mayPlay, isFalse);

        expect(
          controller.requestPlay(expectedIntentRevision: afterPause),
          isTrue,
        );
        expect(controller.mayPlay, isTrue);
      },
    );

    test('A10 cancelling boost alone keeps play intent', () async {
      final ports = _FakePorts();
      final controller = await _openedController(ports);
      final revision = controller.intentRevision;
      controller.beginBoost(eligible: true);
      controller.cancelBoost();
      controller.onPlayingChanged(false);
      expect(controller.intentRevision, revision);
      expect(controller.mayPlay, isTrue);
      expect(controller.rate.temporaryRate, isNull);
    });

    test(
      'A11 explicit play during open survives old wasPlaying=false',
      () async {
        final ports = _FakePorts();
        final controller = await _openedController(ports);
        controller.requestPause();
        final captured = controller.intentRevision;
        await controller.suspendForMediaOpen();
        expect(controller.requestPlay(), isFalse);
        await controller.resumeAfterMediaOpen();
        expect(
          controller.shouldPlayAfterMediaOpen(
            wasPlaying: false,
            capturedIntentRevision: captured,
          ),
          isTrue,
        );
        controller.onPlayingChanged(true);
        expect(
          controller.shouldPlayAfterMediaOpen(
            wasPlaying: false,
            capturedIntentRevision: captured,
          ),
          isFalse,
        );
      },
    );

    test(
      'A11 later pause overrides explicit play pending during open',
      () async {
        final ports = _FakePorts();
        final controller = await _openedController(ports);
        await controller.suspendForMediaOpen();
        controller.requestPlay();
        controller.requestPause();
        await controller.resumeAfterMediaOpen();
        expect(
          controller.shouldPlayAfterMediaOpen(
            wasPlaying: false,
            capturedIntentRevision: controller.intentRevision,
          ),
          isFalse,
        );
      },
    );

    test('A11 reopen applies latest base and old boost stays dead', () async {
      final ports = _FakePorts();
      final controller = await _openedController(ports);
      final session = controller.beginBoost(eligible: true)!;
      await controller.suspendForMediaOpen();
      controller.setBaseRate(1.5);
      await controller.resumeAfterMediaOpen();
      await controller.flushRates();
      session.updateDisplacement(96);
      session.end();
      await controller.flushRates();
      expect(controller.rate.temporaryRate, isNull);
      expect(controller.rate.baseRate, 1.5);
      expect(ports.writes.last, 1.5);
    });

    test(
      'A11 pre-refresh media fetch failure keeps old engine adjustable',
      () async {
        // 取票据失败时未调用 suspend/open，也不调用 mediaOpenFailed。
        final ports = _FakePorts();
        final controller = await _openedController(ports);
        controller.setBaseRate(1.25);
        await _pumpMicrotasks();
        final session = controller.beginBoost(eligible: true);
        expect(session, isNotNull);
        await _pumpMicrotasks();
        session!.end();
        await controller.flushRates();
        expect(ports.writes, <double>[1.25, 2, 1.25]);
      },
    );

    test('A11 suspended then abandoned reopen resumes old channel', () async {
      final ports = _FakePorts();
      final controller = await _openedController(ports);
      await controller.suspendForMediaOpen();
      await controller.resumeAfterMediaOpen();
      await controller.flushRates();
      expect(controller.beginBoost(eligible: true), isNotNull);
    });

    test(
      'A11 open failure keeps channel suspended until next success',
      () async {
        final ports = _FakePorts();
        final controller = await _openedController(ports);
        await controller.suspendForMediaOpen();
        controller.mediaOpenFailed();
        controller.setBaseRate(2);
        await controller.flushRates();
        expect(ports.writes, isEmpty);
        expect(controller.beginBoost(eligible: true), isNull);
        expect(controller.mayPlay, isFalse);
        await controller.resumeAfterMediaOpen();
        await controller.flushRates();
        expect(ports.writes, <double>[2]);
      },
    );
  });
}
