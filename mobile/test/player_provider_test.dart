import 'dart:async';

import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/api/api_client.dart';
import 'package:fan_web/api/media_api.dart';
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
      expect(built.media.uri.toString(), isNot(contains('LOGIN_MUST_NOT_APPEAR')));
      expect(built.media.uri.toString(), isNot(contains('?token=')));
      expect(built.media.start, const Duration(seconds: 125));
      expect(built.expiresAt, DateTime.parse('2026-08-17T12:00:00Z'));
    });

    test('treats MediaTokenUnsupported as a hard fail', () async {
      String? url;
      await expectLater(
        () async {
          final built = await buildPlayerMedia(
            requestMediaToken: (_) async => throw const MediaTokenUnsupported(),
            serverUrl: 'http://127.0.0.1:8080',
            episodeId: 7,
            startPositionSeconds: 0,
          );
          url = built.media.uri.toString();
          return built.media;
        },
        throwsA(isA<MediaTokenUnsupported>()),
      );
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
}
