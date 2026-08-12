import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:fan_web/api/api_client.dart';
import 'package:fan_web/api/progress_api.dart';
import 'package:fan_web/providers/anime_provider.dart';
import 'package:fan_web/providers/auth_provider.dart';
import 'package:fan_web/services/progress_outbox.dart';

void main() {
  late ProviderContainer container;
  late ProgressOutbox outbox;
  late ApiClient apiClient;
  late _RecordingProgressApi progressApi;

  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    apiClient = ApiClient();
    progressApi = _RecordingProgressApi(apiClient);
    container = ProviderContainer(
      overrides: [
        sharedPreferencesProvider.overrideWithValue(prefs),
        progressApiProvider.overrideWithValue(progressApi),
      ],
    );
    outbox = container.read(progressOutboxProvider);
  });

  tearDown(() {
    container.dispose();
    apiClient.dispose();
  });

  group('ProgressOutbox.save', () {
    test('same episode keeps only latest position', () async {
      await outbox.save(
        const PendingProgress(
          serverUrl: 'http://a',
          userId: 1,
          episodeId: 10,
          position: 100,
          watched: false,
          updatedAt: '2026-01-01T00:00:00Z',
        ),
      );
      await outbox.save(
        const PendingProgress(
          serverUrl: 'http://a',
          userId: 1,
          episodeId: 10,
          position: 200,
          watched: false,
          updatedAt: '2026-01-01T00:01:00Z',
        ),
      );
      final pending = await outbox.getPending('http://a', 1);
      expect(pending.length, 1);
      expect(pending.single.position, 200);
    });

    test('watched=true is not overridden by false', () async {
      await outbox.save(
        const PendingProgress(
          serverUrl: 'http://a',
          userId: 1,
          episodeId: 10,
          position: 500,
          watched: true,
          updatedAt: '2026-01-01T00:00:00Z',
        ),
      );
      await outbox.save(
        const PendingProgress(
          serverUrl: 'http://a',
          userId: 1,
          episodeId: 10,
          position: 100,
          watched: false,
          updatedAt: '2026-01-01T00:01:00Z',
        ),
      );
      final pending = await outbox.getPending('http://a', 1);
      expect(pending.single.watched, isTrue);
      expect(pending.single.position, 100);
    });

    test('concurrent writes preserve records for every episode', () async {
      await Future.wait([
        for (var episodeId = 1; episodeId <= 20; episodeId++)
          outbox.save(
            PendingProgress(
              serverUrl: 'http://a',
              userId: 1,
              episodeId: episodeId,
              position: episodeId * 10,
              watched: false,
              updatedAt:
                  '2026-01-01T00:00:${episodeId.toString().padLeft(2, '0')}Z',
            ),
          ),
      ]);

      final pending = await outbox.getPending('http://a', 1);
      expect(pending.length, 20);
      expect(pending.map((record) => record.episodeId).toSet().length, 20);
    });

    test(
      'an older concurrent write cannot roll back latest position',
      () async {
        await Future.wait([
          outbox.save(
            const PendingProgress(
              serverUrl: 'http://a',
              userId: 1,
              episodeId: 10,
              position: 300,
              watched: false,
              updatedAt: '2026-01-01T00:03:00Z',
            ),
          ),
          outbox.save(
            const PendingProgress(
              serverUrl: 'http://a',
              userId: 1,
              episodeId: 10,
              position: 100,
              watched: true,
              updatedAt: '2026-01-01T00:01:00Z',
            ),
          ),
        ]);

        final pending = await outbox.getPending('http://a', 1);
        expect(pending.single.position, 300);
        expect(pending.single.watched, isTrue);
        expect(pending.single.updatedAt, '2026-01-01T00:03:00Z');
      },
    );
  });

  group('ProgressOutbox.removeIfMatched', () {
    test('removes exact match', () async {
      const record = PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 100,
        watched: false,
        updatedAt: '2026-01-01T00:00:00Z',
      );
      await outbox.save(record);
      await outbox.removeIfMatched(record);
      final pending = await outbox.getPending('http://a', 1);
      expect(pending, isEmpty);
    });

    test('does not delete if newer record arrived during send', () async {
      const old = PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 100,
        watched: false,
        updatedAt: '2026-01-01T00:00:00Z',
      );
      await outbox.save(old);
      await outbox.save(
        const PendingProgress(
          serverUrl: 'http://a',
          userId: 1,
          episodeId: 10,
          position: 200,
          watched: false,
          updatedAt: '2026-01-01T00:01:00Z',
        ),
      );
      await outbox.removeIfMatched(old);
      final pending = await outbox.getPending('http://a', 1);
      expect(pending.length, 1);
      expect(pending.single.position, 200);
    });
  });

  group('ProgressOutbox isolation', () {
    test('different users are isolated', () async {
      await outbox.save(
        const PendingProgress(
          serverUrl: 'http://a',
          userId: 1,
          episodeId: 10,
          position: 100,
          watched: false,
          updatedAt: '2026-01-01',
        ),
      );
      await outbox.save(
        const PendingProgress(
          serverUrl: 'http://a',
          userId: 2,
          episodeId: 10,
          position: 200,
          watched: false,
          updatedAt: '2026-01-01',
        ),
      );
      final user1 = await outbox.getPending('http://a', 1);
      final user2 = await outbox.getPending('http://a', 2);
      expect(user1.single.position, 100);
      expect(user2.single.position, 200);
    });

    test('different servers are isolated', () async {
      await outbox.save(
        const PendingProgress(
          serverUrl: 'http://a',
          userId: 1,
          episodeId: 10,
          position: 100,
          watched: false,
          updatedAt: '2026-01-01',
        ),
      );
      await outbox.save(
        const PendingProgress(
          serverUrl: 'http://b',
          userId: 1,
          episodeId: 10,
          position: 200,
          watched: false,
          updatedAt: '2026-01-01',
        ),
      );
      final serverA = await outbox.getPending('http://a', 1);
      final serverB = await outbox.getPending('http://b', 1);
      expect(serverA.single.position, 100);
      expect(serverB.single.position, 200);
    });
  });

  group('ProgressOutbox.clearForUser', () {
    test('removes only that user records', () async {
      await outbox.save(
        const PendingProgress(
          serverUrl: 'http://a',
          userId: 1,
          episodeId: 10,
          position: 100,
          watched: false,
          updatedAt: '2026-01-01',
        ),
      );
      await outbox.save(
        const PendingProgress(
          serverUrl: 'http://a',
          userId: 2,
          episodeId: 10,
          position: 200,
          watched: false,
          updatedAt: '2026-01-01',
        ),
      );
      await outbox.clearForUser(1, 'http://a');
      final user1 = await outbox.getPending('http://a', 1);
      final user2 = await outbox.getPending('http://a', 2);
      expect(user1, isEmpty);
      expect(user2.length, 1);
    });
  });

  group('ProgressOutbox.syncAll', () {
    test('concurrent sync calls do not send the same records twice', () async {
      await outbox.save(
        const PendingProgress(
          serverUrl: 'http://a',
          userId: 1,
          episodeId: 10,
          position: 100,
          watched: false,
          updatedAt: '2026-01-01T00:00:00Z',
        ),
      );
      await outbox.save(
        const PendingProgress(
          serverUrl: 'http://a',
          userId: 1,
          episodeId: 11,
          position: 200,
          watched: false,
          updatedAt: '2026-01-01T00:01:00Z',
        ),
      );

      await Future.wait([
        outbox.syncAll('http://a', 1, 'token'),
        outbox.syncAll('http://a', 1, 'token'),
      ]);

      expect(progressApi.reportedEpisodeIds, [10, 11]);
      expect(progressApi.maxConcurrentReports, 1);
      expect(await outbox.getPending('http://a', 1), isEmpty);
    });

    test(
      'record saved while sending is not removed with stale request',
      () async {
        const oldRecord = PendingProgress(
          serverUrl: 'http://a',
          userId: 1,
          episodeId: 10,
          position: 100,
          watched: false,
          updatedAt: '2026-01-01T00:00:00Z',
        );
        await outbox.save(oldRecord);
        progressApi.pauseReports();

        final sync = outbox.syncAll('http://a', 1, 'token');
        await progressApi.reportStarted.future;
        await outbox.save(
          const PendingProgress(
            serverUrl: 'http://a',
            userId: 1,
            episodeId: 10,
            position: 200,
            watched: false,
            updatedAt: '2026-01-01T00:01:00Z',
          ),
        );
        progressApi.resumeReports();
        await sync;

        final pending = await outbox.getPending('http://a', 1);
        expect(pending.single.position, 200);
      },
    );
  });

  group('ProgressOutbox.syncAll error handling', () {
    const record10 = PendingProgress(
      serverUrl: 'http://a',
      userId: 1,
      episodeId: 10,
      position: 100,
      watched: false,
      updatedAt: '2026-01-01T00:00:00Z',
    );
    const record11 = PendingProgress(
      serverUrl: 'http://a',
      userId: 1,
      episodeId: 11,
      position: 200,
      watched: false,
      updatedAt: '2026-01-01T00:01:00Z',
    );

    test('permanent 1002 removes the record and continues to next', () async {
      await outbox.save(record10);
      await outbox.save(record11);
      progressApi.errorsByEpisode[10] =
          const ApiException(1002, '集数不存在');

      await outbox.syncAll('http://a', 1, 'token');

      // 第二条记录仍被上报，说明永久失败没有中断整轮同步。
      expect(progressApi.reportedEpisodeIds, [10, 11]);
      // 失败记录（集 10）被移除；集 11 上报成功也被移除，故 outbox 为空。
      // 若失败记录未被移除，这里会残留 episodeId == 10。
      final pending = await outbox.getPending('http://a', 1);
      expect(pending, isEmpty);
    });

    test('network timeout stops the round and keeps both records', () async {
      await outbox.save(record10);
      await outbox.save(record11);
      progressApi.errorsByEpisode[10] = DioException(
        requestOptions: RequestOptions(path: '/api/progress/10'),
        type: DioExceptionType.connectionTimeout,
      );

      await outbox.syncAll('http://a', 1, 'token');

      // 第二条记录未被上报，说明整轮同步被终止。
      expect(progressApi.reportedEpisodeIds, [10]);
      // 两条记录都保留，等待网络恢复后的下次同步。
      expect(await outbox.getPending('http://a', 1), hasLength(2));
    });

    test('401 business error stops the round', () async {
      await outbox.save(record10);
      await outbox.save(record11);
      progressApi.errorsByEpisode[10] =
          const ApiException(2001, '登录状态已失效');

      await outbox.syncAll('http://a', 1, 'token');

      expect(progressApi.reportedEpisodeIds, [10]);
      expect(await outbox.getPending('http://a', 1), hasLength(2));
    });

    test('server 5xx badResponse stops the round', () async {
      await outbox.save(record10);
      await outbox.save(record11);
      final options = RequestOptions(path: '/api/progress/10');
      progressApi.errorsByEpisode[10] = DioException(
        requestOptions: options,
        response: Response(requestOptions: options, statusCode: 500),
        type: DioExceptionType.badResponse,
      );

      await outbox.syncAll('http://a', 1, 'token');

      expect(progressApi.reportedEpisodeIds, [10]);
      expect(await outbox.getPending('http://a', 1), hasLength(2));
    });

    test('unknown error stops the round', () async {
      await outbox.save(record10);
      await outbox.save(record11);
      progressApi.errorsByEpisode[10] = StateError('未知异常');

      await outbox.syncAll('http://a', 1, 'token');

      expect(progressApi.reportedEpisodeIds, [10]);
      expect(await outbox.getPending('http://a', 1), hasLength(2));
    });

    test('permanently failed record is not resent on later sync', () async {
      await outbox.save(record10);
      progressApi.errorsByEpisode[10] =
          const ApiException(1002, '集数不存在');

      await outbox.syncAll('http://a', 1, 'token');
      expect(progressApi.reportedEpisodeIds, [10]);
      expect(await outbox.getPending('http://a', 1), isEmpty);

      // 记录已被移除，第二轮同步不应再次上报它。
      await outbox.syncAll('http://a', 1, 'token');
      expect(progressApi.reportedEpisodeIds, [10]);
    });
  });
}

class _RecordingProgressApi extends ProgressApi {
  _RecordingProgressApi(super.client);

  final List<int> reportedEpisodeIds = [];
  int activeReports = 0;
  int maxConcurrentReports = 0;
  Completer<void> reportStarted = Completer<void>();
  Completer<void>? _reportRelease;

  /// 按集数配置本次上报要抛出的错误；未配置的集数正常上报成功。
  /// 上报仍会记录到 reportedEpisodeIds，仅抛出配置的错误。
  final Map<int, Object?> errorsByEpisode = {};

  void pauseReports() {
    reportStarted = Completer<void>();
    _reportRelease = Completer<void>();
  }

  void resumeReports() {
    _reportRelease?.complete();
    _reportRelease = null;
  }

  @override
  Future<void> reportProgress(int episodeId, int position, bool watched) async {
    reportedEpisodeIds.add(episodeId);
    activeReports++;
    if (activeReports > maxConcurrentReports) {
      maxConcurrentReports = activeReports;
    }
    if (!reportStarted.isCompleted) {
      reportStarted.complete();
    }
    try {
      // 先按配置抛出错误，模拟业务/网络异常；reportedEpisodeIds 已记录。
      final error = errorsByEpisode[episodeId];
      if (error != null) {
        throw error;
      }
      final release = _reportRelease;
      if (release != null) {
        await release.future;
      } else {
        await Future<void>.delayed(Duration.zero);
      }
    } finally {
      activeReports--;
    }
  }
}
