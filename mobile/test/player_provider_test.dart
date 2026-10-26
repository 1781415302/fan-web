import 'dart:async';

import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/api/api_client.dart';
import 'package:fan_web/api/media_api.dart';
import 'package:fan_web/models/anime.dart';
import 'package:fan_web/models/playback_interaction.dart' as model;
import 'package:fan_web/providers/player_provider.dart';

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
}
