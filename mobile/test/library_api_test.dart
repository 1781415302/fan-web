import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/api/api_client.dart';
import 'package:fan_web/api/library_api.dart';
import 'package:fan_web/utils/api_error.dart';

import 'http_stub.dart';

void main() {
  group('LibraryApi', () {
    test('startScan POSTs /library/scan', () async {
      RequestOptions? seen;
      final client = clientWithHandler((options) {
        seen = options;
        return jsonBody(
          ok(<String, dynamic>{
            'state': 'running',
            'started_at': '2026-08-20T06:00:00Z',
          }),
        );
      });
      final job = await LibraryApi(client).startScan();
      expect(seen!.method, 'POST');
      expect(seen!.path, 'library/scan');
      expect(job.state, 'running');
      expect(job.isRunning, isTrue);
    });

    test('getScan GETs /library/scan', () async {
      RequestOptions? seen;
      final client = clientWithHandler((options) {
        seen = options;
        return jsonBody(ok(<String, dynamic>{'state': 'idle'}));
      });
      final job = await LibraryApi(client).getScan();
      expect(seen!.method, 'GET');
      expect(seen!.path, 'library/scan');
      expect(job.state, 'idle');
    });

    test('listUnidentified parses items', () async {
      final client = clientWith(
        ok(<String, dynamic>{
          'items': <Map<String, dynamic>>[
            {
              'file_name': 'ep01.mkv',
              'reason': 'ambiguous',
              'file_path': 'ShowDir',
              'candidates': <Map<String, dynamic>>[
                {'id': 101, 'name': 'Show', 'name_cn': '候选番剧', 'score': 0.91},
              ],
            },
          ],
          'total': 1,
          'page': 1,
          'page_size': 50,
        }),
      );
      final result = await LibraryApi(client).listUnidentified();
      expect(result.total, 1);
      expect(result.items.single.filePath, 'ShowDir');
      expect(result.items.single.candidates.single.displayName, '候选番剧');
    });

    test('listDirs returns items and drops empty strings', () async {
      RequestOptions? seen;
      final client = clientWithHandler((options) {
        seen = options;
        return jsonBody(
          ok(<String, dynamic>{
            'items': <String>['ShowA', '', 'ShowB'],
          }),
        );
      });
      final dirs = await LibraryApi(client).listDirs();
      expect(seen!.method, 'GET');
      expect(seen!.path, 'library/dirs');
      expect(dirs, ['ShowA', 'ShowB']);
    });
  });

  group('LibraryApi.confirmInbox', () {
    Map<String, dynamic> confirmErrorEnvelope(
      int code,
      String message,
      Object? data,
    ) => <String, dynamic>{'code': code, 'message': message, 'data': data};

    test(
      'posts request body with 600s receiveTimeout and parses result',
      () async {
        RequestOptions? seen;
        final client = clientWithHandler((options) {
          seen = options;
          return jsonBody(
            ok(<String, dynamic>{
              'anime': <String, dynamic>{
                'id': 42,
                'title': 'Show',
                'title_cn': '番剧',
                'bangumi_id': 101,
                'cover': '',
                'summary': '',
                'ep_count': 12,
                'file_path': '',
                'created_at': '2026-08-20T00:00:00Z',
              },
              'created': true,
              'scanned': 2,
              'episodes': <Map<String, dynamic>>[
                {
                  'id': 1,
                  'anime_id': 42,
                  'ep_number': 1,
                  'title': '01',
                  'file_path': 'ep01.mkv',
                  'duration': 0,
                },
                {
                  'id': 2,
                  'anime_id': 42,
                  'ep_number': 2,
                  'title': '02',
                  'file_path': 'ep02.mkv',
                  'duration': 0,
                },
              ],
            }),
          );
        });
        final result = await LibraryApi(client).confirmInbox(
          bangumiId: 101,
          filePath: '',
          fileNames: const ['ep01.mkv', 'ep02.mkv'],
          episodeOffset: 12,
        );
        expect(seen!.method, 'POST');
        expect(seen!.path, 'library/inbox/confirm');
        expect(seen!.receiveTimeout, const Duration(seconds: 600));
        expect(seen!.data, <String, dynamic>{
          'bangumi_id': 101,
          'file_path': '',
          'file_names': ['ep01.mkv', 'ep02.mkv'],
          'episode_offset': 12,
        });
        expect(result.anime.id, 42);
        expect(result.anime.bangumiId, 101);
        expect(result.created, isTrue);
        expect(result.scanned, 2);
        expect(result.episodes.map((e) => e.filePath), [
          'ep01.mkv',
          'ep02.mkv',
        ]);
      },
    );

    test('episode_offset defaults to 0', () async {
      RequestOptions? seen;
      final client = clientWithHandler((options) {
        seen = options;
        return jsonBody(
          ok(<String, dynamic>{
            'anime': <String, dynamic>{'id': 7, 'bangumi_id': 101},
            'created': false,
            'scanned': 0,
            'episodes': <Object>[],
          }),
        );
      });
      final result = await LibraryApi(client).confirmInbox(
        bangumiId: 101,
        filePath: 'ShowDir',
        fileNames: const ['ep01.mkv'],
      );
      expect((seen!.data as Map)['episode_offset'], 0);
      expect(result.created, isFalse);
      expect(result.episodes, isEmpty);
    });

    test('1001 without reason falls back to invalid', () async {
      final client = clientWith(errorEnvelope(1001, '参数错误'));
      await expectLater(
        LibraryApi(client).confirmInbox(
          bangumiId: 101,
          filePath: 'ShowDir',
          fileNames: const ['ep01.mkv'],
        ),
        throwsA(
          isA<ConfirmInboxException>()
              .having((e) => e.code, 'code', 1001)
              .having((e) => e.message, 'message', '参数错误')
              .having((e) => e.reason, 'reason', 'invalid')
              .having((e) => e.fileNames, 'fileNames', isEmpty),
        ),
      );
    });

    test('9999 without reason falls back to upstream', () async {
      final client = clientWith(errorEnvelope(9999, '服务器错误'));
      await expectLater(
        LibraryApi(client).confirmInbox(
          bangumiId: 101,
          filePath: 'ShowDir',
          fileNames: const ['ep01.mkv'],
        ),
        throwsA(
          isA<ConfirmInboxException>()
              .having((e) => e.code, 'code', 9999)
              .having((e) => e.reason, 'reason', 'upstream'),
        ),
      );
    });

    test('keeps reason and file_names from envelope data', () async {
      final client = clientWith(
        confirmErrorEnvelope(9999, '文件已归属其他番剧', <String, dynamic>{
          'reason': 'conflict',
          'message': '文件已归属其他番剧',
          'file_names': <Object>['ep01.mkv', 3, 'ep02.mkv'],
        }),
      );
      Object? caught;
      try {
        await LibraryApi(client).confirmInbox(
          bangumiId: 101,
          filePath: 'ShowDir',
          fileNames: const ['ep01.mkv', 'ep02.mkv'],
        );
      } catch (error) {
        caught = error;
      }
      // 不经 _request：_request 只会抛出裸 ApiException 并丢失 reason。
      expect(caught, isA<ConfirmInboxException>());
      final error = caught! as ConfirmInboxException;
      expect(error, isA<ApiException>());
      expect(error.code, 9999);
      expect(error.reason, 'conflict');
      expect(error.fileNames, ['ep01.mkv', 'ep02.mkv']);
      expect(describeApiError(error), isNotEmpty);
    });

    test('1001 with explicit reason keeps it', () async {
      final client = clientWith(
        confirmErrorEnvelope(1001, '目录不一致', <String, dynamic>{
          'reason': 'dir_mismatch',
          'message': '目录不一致',
        }),
      );
      await expectLater(
        LibraryApi(client).confirmInbox(
          bangumiId: 101,
          filePath: 'Other',
          fileNames: const ['ep01.mkv'],
        ),
        throwsA(
          isA<ConfirmInboxException>()
              .having((e) => e.reason, 'reason', 'dir_mismatch')
              .having((e) => e.message, 'message', '目录不一致'),
        ),
      );
    });

    test('non-ApiException DioException is rethrown as-is', () async {
      final client = clientWithHandler((options) {
        throw DioException.connectionError(
          requestOptions: options,
          reason: 'refused',
        );
      });
      await expectLater(
        LibraryApi(client).confirmInbox(
          bangumiId: 101,
          filePath: 'ShowDir',
          fileNames: const ['ep01.mkv'],
        ),
        throwsA(
          isA<DioException>()
              .having((e) => e.type, 'type', DioExceptionType.connectionError)
              .having((e) => e.error, 'error', isNot(isA<ApiException>())),
        ),
      );
    });
  });

  group('pollLibraryScan', () {
    test('POSTs then GETs every interval until done', () async {
      final calls = <String>[];
      var gets = 0;
      final client = clientWithHandler((options) {
        calls.add('${options.method} ${options.path}');
        if (options.method == 'POST') {
          return jsonBody(ok(<String, dynamic>{'state': 'running'}));
        }
        gets += 1;
        if (gets < 2) {
          return jsonBody(ok(<String, dynamic>{'state': 'running'}));
        }
        return jsonBody(
          ok(<String, dynamic>{
            'state': 'done',
            'result': <String, dynamic>{
              'total_files': 3,
              'skipped': 1,
              'new_animes': 1,
              'new_episodes': 2,
              'unidentified': <Object>[],
            },
          }),
        );
      });
      final delays = <Duration>[];
      final job = await pollLibraryScan(
        api: LibraryApi(client),
        delay: (duration) async {
          delays.add(duration);
        },
      );
      expect(job.state, 'done');
      expect(job.result?.newAnimes, 1);
      expect(calls, [
        'POST library/scan',
        'GET library/scan',
        'GET library/scan',
      ]);
      expect(delays, hasLength(2));
      expect(
        delays.every((item) => item == const Duration(seconds: 1)),
        isTrue,
      );
    });

    test('stops polling after 15 minutes while still running', () async {
      var now = DateTime.utc(2026, 8, 20);
      final calls = <String>[];
      final client = clientWithHandler((options) {
        calls.add('${options.method} ${options.path}');
        return jsonBody(ok(<String, dynamic>{'state': 'running'}));
      });
      final job = await pollLibraryScan(
        api: LibraryApi(client),
        clock: () => now,
        delay: (duration) async {
          now = now.add(const Duration(minutes: 16));
        },
      );
      expect(job.state, 'running');
      expect(calls.first, 'POST library/scan');
      expect(calls.where((item) => item.startsWith('GET')), isNotEmpty);
      expect(calls.length, lessThan(5));
    });

    test('stops immediately on error', () async {
      final client = clientWithHandler((options) {
        if (options.method == 'POST') {
          return jsonBody(ok(<String, dynamic>{'state': 'running'}));
        }
        return jsonBody(
          ok(<String, dynamic>{'state': 'error', 'error': 'boom'}),
        );
      });
      final job = await pollLibraryScan(
        api: LibraryApi(client),
        delay: (_) async {},
      );
      expect(job.state, 'error');
      expect(job.error, 'boom');
    });
  });
}
