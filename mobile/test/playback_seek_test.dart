import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/models/playback_seek.dart';

void main() {
  group('seek constants', () {
    test('keeps the locked step, margin, window and speed', () {
      expect(skipForwardStep, const Duration(seconds: 80));
      expect(seekEndMargin, const Duration(seconds: 1));
      expect(seekBaselineWindow, const Duration(milliseconds: 1500));
      expect(horizontalSeekSecondsPerLogicalPixel, 0.3);
    });
  });

  group('horizontalDragSeekDelta', () {
    test('maps right drag to forward seconds', () {
      expect(horizontalDragSeekDelta(100), const Duration(seconds: 30));
      expect(horizontalDragSeekDelta(400), const Duration(seconds: 120));
    });

    test('maps left drag to backward seconds', () {
      expect(horizontalDragSeekDelta(-100), const Duration(seconds: -30));
    });

    test('rounds tiny drags to zero', () {
      expect(horizontalDragSeekDelta(1), Duration.zero);
      expect(horizontalDragSeekDelta(0), Duration.zero);
    });

    test('returns zero for non-finite input', () {
      expect(horizontalDragSeekDelta(double.nan), Duration.zero);
      expect(horizontalDragSeekDelta(double.infinity), Duration.zero);
      expect(horizontalDragSeekDelta(double.negativeInfinity), Duration.zero);
    });
  });
}
