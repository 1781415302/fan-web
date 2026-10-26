import 'package:fan_web/models/playback_interaction.dart';
import 'package:fan_web/widgets/player_gesture_surface.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class _FakeBoostSession implements TemporaryBoostSession {
  _FakeBoostSession(this.id);

  @override
  final int id;
  final List<double> displacements = <double>[];
  int endCount = 0;

  @override
  void updateDisplacement(double horizontalDx) {
    displacements.add(horizontalDx);
  }

  @override
  void end() {
    endCount++;
  }
}

/// 记录全部转发回调；pan 完成逻辑沿用原测试的 panStarted 防护写法。
class _Recorder {
  final List<_FakeBoostSession> sessions = <_FakeBoostSession>[];
  int boostStartCalls = 0;
  bool returnNullBoost = false;
  int tapCount = 0;
  int doubleTapCount = 0;
  int panStartCount = 0;
  int panUpdateCount = 0;
  int panEndCount = 0;
  int panCancelCount = 0;
  int panCompletionCount = 0;
  int interactionStartCount = 0;
  int interactionEndCount = 0;
  bool panStarted = false;
  VoidCallback? onPanStartHook;

  TemporaryBoostSession? boostStart() {
    boostStartCalls++;
    if (returnNullBoost) {
      return null;
    }
    final session = _FakeBoostSession(sessions.length + 1);
    sessions.add(session);
    return session;
  }
}

class _Harness extends StatefulWidget {
  const _Harness({required this.recorder, this.withControl = false});

  final _Recorder recorder;
  final bool withControl;

  @override
  State<_Harness> createState() => _HarnessState();
}

class _HarnessState extends State<_Harness> {
  Object sessionIdentity = Object();
  bool enabled = true;
  bool boostEnabled = true;
  bool mounted_ = true;
  int controlPressCount = 0;

  void update(VoidCallback change) => setState(change);

  @override
  Widget build(BuildContext context) {
    final r = widget.recorder;
    return Directionality(
      textDirection: TextDirection.ltr,
      child: Stack(
        children: [
          if (mounted_)
            Positioned.fill(
              child: PlayerGestureSurface(
                key: const ValueKey('surface'),
                sessionIdentity: sessionIdentity,
                enabled: enabled,
                boostEnabled: boostEnabled,
                // 每次 build 都是新 closure，验证普通 rebuild 不取消手势。
                onBoostStart: () => r.boostStart(),
                onTap: () => r.tapCount++,
                onDoubleTap: () => r.doubleTapCount++,
                onPanStart: (_) {
                  r.panStartCount++;
                  r.panStarted = true;
                  r.onPanStartHook?.call();
                },
                onPanUpdate: (_) => r.panUpdateCount++,
                onPanEnd: (_) {
                  r.panEndCount++;
                  if (r.panStarted) {
                    r.panStarted = false;
                    r.panCompletionCount++;
                  }
                },
                onPanCancel: () {
                  r.panCancelCount++;
                  if (r.panStarted) {
                    r.panStarted = false;
                    r.panCompletionCount++;
                  }
                },
                onInteractionStart: () => r.interactionStartCount++,
                onInteractionEnd: () => r.interactionEndCount++,
              ),
            ),
          if (widget.withControl)
            Positioned(
              left: 0,
              top: 0,
              width: 120,
              height: 60,
              child: GestureDetector(
                key: const ValueKey('control'),
                behavior: HitTestBehavior.opaque,
                onTap: () => controlPressCount++,
                child: const ColoredBox(color: Color(0xFF000000)),
              ),
            ),
        ],
      ),
    );
  }
}

const Offset _center = Offset(400, 300);
const Duration _longPress = kLongPressTimeout;
const Duration _afterLongPress = Duration(milliseconds: 20);

Future<_HarnessState> _mount(
  WidgetTester tester,
  _Recorder recorder, {
  bool withControl = false,
}) async {
  await tester.pumpWidget(
    _Harness(recorder: recorder, withControl: withControl),
  );
  return tester.state<_HarnessState>(find.byType(_Harness));
}

Future<TestGesture> _longPressDown(WidgetTester tester, {Offset? at}) async {
  final gesture = await tester.startGesture(at ?? _center);
  await tester.pump(_longPress);
  await tester.pump(_afterLongPress);
  return gesture;
}

void main() {
  group('gesture surface', () {
    testWidgets('a tap does not run pan completion logic', (tester) async {
      final r = _Recorder();
      await _mount(tester, r);

      await tester.tapAt(_center);
      await tester.pump(kDoubleTapTimeout + const Duration(milliseconds: 10));

      expect(r.tapCount, 1);
      expect(r.panCompletionCount, 0);
      expect(r.panStartCount, 0);
      expect(r.boostStartCalls, 0);
      expect(r.interactionStartCount, 1);
      expect(r.interactionEndCount, 1);
    });

    testWidgets('A12 long press starts once, moves adjust rate, up restores', (
      tester,
    ) async {
      final r = _Recorder();
      await _mount(tester, r);

      final gesture = await tester.startGesture(_center);
      await tester.pump(_longPress - const Duration(milliseconds: 50));
      expect(r.boostStartCalls, 0);
      await tester.pump(const Duration(milliseconds: 60));
      expect(r.boostStartCalls, 1);

      await gesture.moveBy(const Offset(48, 0));
      await tester.pump();
      await gesture.moveBy(const Offset(48, 0));
      await tester.pump();
      expect(r.sessions.single.displacements.last, closeTo(96, 0.001));

      await gesture.up();
      await tester.pump(kDoubleTapTimeout + const Duration(milliseconds: 10));

      expect(r.boostStartCalls, 1);
      expect(r.sessions.single.endCount, greaterThanOrEqualTo(1));
      expect(r.tapCount, 0);
      expect(r.doubleTapCount, 0);
      expect(r.panStartCount, 0);
      expect(r.panCompletionCount, 0);
      expect(r.interactionEndCount, 1);
    });

    testWidgets('A13 PointerCancel after long press won restores boost', (
      tester,
    ) async {
      final r = _Recorder();
      await _mount(tester, r);

      final gesture = await _longPressDown(tester);
      await gesture.moveBy(const Offset(96, 0));
      await tester.pump();
      expect(r.sessions.single.endCount, 0);

      await gesture.cancel();
      await tester.pump();

      expect(r.sessions.single.endCount, greaterThanOrEqualTo(1));
      expect(r.panCompletionCount, 0);
      expect(r.interactionEndCount, 1);
    });

    testWidgets('A13 failed long press cancel does not run pan completion', (
      tester,
    ) async {
      final r = _Recorder();
      await _mount(tester, r);

      final gesture = await tester.startGesture(_center);
      await tester.pump(const Duration(milliseconds: 100));
      await gesture.cancel();
      await tester.pump(_longPress);

      expect(r.boostStartCalls, 0);
      expect(r.panCompletionCount, 0);
      expect(r.tapCount, 0);
      expect(r.interactionEndCount, 1);
    });

    testWidgets('A14 movement before long press goes to pan and survives '
        'boostEnabled=false', (tester) async {
      final r = _Recorder();
      final harness = await _mount(tester, r);
      // 普通水平 pan 会主动 pause，使父级 boostEnabled=false。
      r.onPanStartHook = () =>
          harness.update(() => harness.boostEnabled = false);

      final gesture = await tester.startGesture(_center);
      await gesture.moveBy(const Offset(40, 0));
      await tester.pump();
      await gesture.moveBy(const Offset(40, 0));
      await tester.pump();
      await tester.pump(_longPress);
      await gesture.moveBy(const Offset(20, 0));
      await tester.pump();
      await gesture.up();
      await tester.pump();

      expect(r.panStartCount, 1);
      expect(r.panUpdateCount, greaterThanOrEqualTo(1));
      expect(r.panCompletionCount, 1);
      expect(r.boostStartCalls, 0);
      expect(r.interactionEndCount, 1);
    });

    testWidgets('A14 vertical move after long press does not pan', (
      tester,
    ) async {
      final r = _Recorder();
      await _mount(tester, r);

      final gesture = await _longPressDown(tester);
      await gesture.moveBy(const Offset(0, -150));
      await tester.pump();
      await gesture.moveBy(const Offset(0, 150));
      await tester.pump();
      await gesture.up();
      await tester.pump();

      expect(r.panStartCount, 0);
      expect(r.panUpdateCount, 0);
      expect(r.panCompletionCount, 0);
      expect(r.sessions.single.displacements, everyElement(closeTo(0, 0.001)));
      expect(r.sessions.single.endCount, greaterThanOrEqualTo(1));
    });

    testWidgets('A14 second finger cancels boost and blocks re-entry', (
      tester,
    ) async {
      final r = _Recorder();
      await _mount(tester, r);

      final first = await _longPressDown(tester);
      expect(r.boostStartCalls, 1);
      final second = await tester.startGesture(
        _center + const Offset(100, 0),
        pointer: 1007,
      );
      await tester.pump();
      expect(r.sessions.single.endCount, 1);

      await first.moveBy(const Offset(120, 0));
      await tester.pump();
      await first.up();
      await tester.pump(_longPress);
      // 第二指仍按住：不重入 boost、不变成 pan。
      expect(r.boostStartCalls, 1);
      expect(r.panStartCount, 0);
      expect(r.interactionEndCount, 0);

      await second.up();
      await tester.pump();
      expect(r.interactionEndCount, 1);

      // 全部抬起后新触摸可以再次开始。
      final again = await _longPressDown(tester);
      expect(r.boostStartCalls, 2);
      await again.up();
      await tester.pump();
    });

    testWidgets('A14 second finger after pan started cancels pan once', (
      tester,
    ) async {
      final r = _Recorder();
      await _mount(tester, r);

      final first = await tester.startGesture(_center);
      await first.moveBy(const Offset(60, 0));
      await tester.pump();
      expect(r.panStartCount, 1);
      final second = await tester.startGesture(
        _center + const Offset(0, 80),
        pointer: 1009,
      );
      await tester.pump();
      expect(r.panCancelCount, 1);
      await first.moveBy(const Offset(60, 0));
      await first.up();
      await second.up();
      await tester.pump(kDoubleTapTimeout + const Duration(milliseconds: 10));
      expect(r.panCancelCount, 1);
      expect(r.panEndCount, 0);
      expect(r.interactionEndCount, 1);
    });

    testWidgets('A15 enabled=false ends active boost', (tester) async {
      final r = _Recorder();
      final harness = await _mount(tester, r);

      final gesture = await _longPressDown(tester);
      harness.update(() => harness.enabled = false);
      await tester.pump();
      expect(r.sessions.single.endCount, 1);
      await gesture.moveBy(const Offset(100, 0));
      await tester.pump();
      expect(r.sessions.single.displacements, isEmpty);
      await gesture.up();
      await tester.pump();
      expect(r.interactionEndCount, lessThanOrEqualTo(1));
      expect(tester.takeException(), isNull);
    });

    testWidgets('A15 boostEnabled=false ends active boost', (tester) async {
      final r = _Recorder();
      final harness = await _mount(tester, r);

      final gesture = await _longPressDown(tester);
      harness.update(() => harness.boostEnabled = false);
      await tester.pump();
      expect(r.sessions.single.endCount, 1);
      await gesture.moveBy(const Offset(100, 0));
      await tester.pump();
      expect(r.sessions.single.displacements, isEmpty);
      await gesture.up();
      await tester.pump();
    });

    testWidgets('A15 sessionIdentity change ends old handle only', (
      tester,
    ) async {
      final r = _Recorder();
      final harness = await _mount(tester, r);

      final gesture = await _longPressDown(tester);
      harness.update(() => harness.sessionIdentity = Object());
      await tester.pump();
      expect(r.sessions.single.endCount, 1);
      await gesture.moveBy(const Offset(100, 0));
      await tester.pump(_longPress);
      await gesture.up();
      await tester.pump();
      // 旧序列不能在新会话上启动 boost。
      expect(r.boostStartCalls, 1);
      expect(r.sessions.single.displacements, isEmpty);
    });

    testWidgets('A15 dispose ends old handle without exception', (
      tester,
    ) async {
      final r = _Recorder();
      final harness = await _mount(tester, r);

      final gesture = await _longPressDown(tester);
      harness.update(() => harness.mounted_ = false);
      await tester.pump();
      expect(r.sessions.single.endCount, 1);
      await gesture.up();
      await tester.pump();
      expect(tester.takeException(), isNull);
    });

    testWidgets('A15 plain rebuild with new closures keeps gesture', (
      tester,
    ) async {
      final r = _Recorder();
      final harness = await _mount(tester, r);

      final gesture = await _longPressDown(tester);
      harness.update(() {});
      await tester.pump();
      await gesture.moveBy(const Offset(48, 0));
      await tester.pump();
      expect(r.sessions.single.endCount, 0);
      expect(r.sessions.single.displacements.last, closeTo(48, 0.001));
      await gesture.up();
      await tester.pump();
      expect(r.sessions.single.endCount, greaterThanOrEqualTo(1));
    });

    testWidgets('A16 holding on control region does not start boost', (
      tester,
    ) async {
      final r = _Recorder();
      final harness = await _mount(tester, r, withControl: true);

      final gesture = await _longPressDown(tester, at: const Offset(60, 30));
      await gesture.up();
      await tester.pump(kDoubleTapTimeout + const Duration(milliseconds: 10));
      expect(r.boostStartCalls, 0);
      expect(r.interactionStartCount, 0);
      final pressesBefore = harness.controlPressCount;

      await tester.tapAt(const Offset(60, 30));
      await tester.pump(kDoubleTapTimeout + const Duration(milliseconds: 10));
      expect(harness.controlPressCount, pressesBefore + 1);
      expect(r.tapCount, 0);
    });

    testWidgets('A16 paused cannot boost but double tap still works', (
      tester,
    ) async {
      final r = _Recorder();
      final harness = await _mount(tester, r);
      harness.update(() => harness.boostEnabled = false);
      await tester.pump();

      final gesture = await _longPressDown(tester);
      await gesture.moveBy(const Offset(96, 0));
      await gesture.up();
      await tester.pump(kDoubleTapTimeout + const Duration(milliseconds: 10));
      expect(r.boostStartCalls, 0);
      expect(r.panCompletionCount, 0);

      await tester.tapAt(_center);
      await tester.pump(kDoubleTapMinTime + const Duration(milliseconds: 10));
      await tester.tapAt(_center);
      await tester.pump(kDoubleTapTimeout + const Duration(milliseconds: 10));
      expect(r.doubleTapCount, 1);
    });
  });
}
