import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/api/library_api.dart';

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
