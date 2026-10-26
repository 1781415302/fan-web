import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/models/playback_interaction.dart';

void main() {
  group('rate model', () {
    test('keeps the five normal rates and falls back to 1', () {
      expect(playbackRateOptions, <double>[0.5, 1, 1.25, 1.5, 2]);
      for (final rate in playbackRateOptions) {
        expect(normalizePlaybackRate(rate), rate);
      }
      expect(normalizePlaybackRate(3), 1);
      expect(normalizePlaybackRate(2.5), 1);
      expect(normalizePlaybackRate(0), 1);
      expect(normalizePlaybackRate(double.nan), 1);
      expect(normalizePlaybackRate(double.infinity), 1);
    });

    test('formats normal rates like the old menu', () {
      expect(playbackRateLabel(0.5), '0.5x');
      expect(playbackRateLabel(1), '1x');
      expect(playbackRateLabel(1.25), '1.25x');
      expect(playbackRateLabel(1.5), '1.5x');
      expect(playbackRateLabel(2), '2x');
      expect(playbackRateLabel(3), '1x');
      expect(playbackRateLabel(double.nan), '1x');
    });

    test('keeps the five temporary rates and formats 3x', () {
      expect(temporaryPlaybackRateOptions, <double>[1.25, 1.5, 2, 2.5, 3]);
      expect(defaultTemporaryPlaybackRate, 2);
      expect(temporaryRateStepDistance, 48);
      expect(temporaryPlaybackRateLabel(1.25), '1.25x');
      expect(temporaryPlaybackRateLabel(1.5), '1.5x');
      expect(temporaryPlaybackRateLabel(2), '2x');
      expect(temporaryPlaybackRateLabel(2.5), '2.5x');
      expect(temporaryPlaybackRateLabel(3), '3x');
      expect(temporaryPlaybackRateLabel(0.5), '2x');
      expect(temporaryPlaybackRateLabel(double.nan), '2x');
    });

    test('maps cumulative displacement to temporary rates', () {
      expect(temporaryRateForDisplacement(0), 2);
      expect(temporaryRateForDisplacement(48), 2.5);
      expect(temporaryRateForDisplacement(96), 3);
      expect(temporaryRateForDisplacement(-48), 1.5);
      expect(temporaryRateForDisplacement(-96), 1.25);
      expect(temporaryRateForDisplacement(10), 2);
      expect(temporaryRateForDisplacement(-10), 2);
    });

    test('clamps out-of-range and non-finite displacement', () {
      expect(temporaryRateForDisplacement(480), 3);
      expect(temporaryRateForDisplacement(-480), 1.25);
      expect(temporaryRateForDisplacement(double.nan), 2);
      expect(temporaryRateForDisplacement(double.infinity), 2);
      expect(temporaryRateForDisplacement(double.negativeInfinity), 2);
    });

    test('snapshot effective rate prefers temporary rate', () {
      const idle = PlaybackRateSnapshot(
        baseRate: 1.25,
        temporaryRate: null,
        boostSessionId: null,
      );
      expect(idle.effectiveRate, 1.25);
      const boosting = PlaybackRateSnapshot(
        baseRate: 1.25,
        temporaryRate: 3,
        boostSessionId: 1,
      );
      expect(boosting.effectiveRate, 3);
      expect(boosting.baseRate, 1.25);
    });
  });
}
