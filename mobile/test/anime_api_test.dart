import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/api/anime_api.dart';
import 'package:fan_web/api/api_client.dart';

import 'http_stub.dart';

void main() {
  group('AnimeApi.list', () {
    test('passes keyword query parameter', () async {
      RequestOptions? seen;
      final client = clientWithHandler((options) {
        seen = options;
        return jsonBody(
          ok(<String, dynamic>{
            'items': <Object>[],
            'total': 0,
            'page': 1,
            'page_size': 20,
          }),
        );
      });
      await AnimeApi(client).list(page: 2, pageSize: 10, keyword: '  进击  ');
      expect(seen!.path, 'animes');
      expect(seen!.queryParameters['page'], 2);
      expect(seen!.queryParameters['page_size'], 10);
      expect(seen!.queryParameters['keyword'], '进击');
    });

    test('omits empty keyword', () async {
      RequestOptions? seen;
      final client = clientWithHandler((options) {
        seen = options;
        return jsonBody(
          ok(<String, dynamic>{
            'items': <Object>[],
            'total': 0,
            'page': 1,
            'page_size': 20,
          }),
        );
      });
      await AnimeApi(client).list();
      expect(seen!.queryParameters.containsKey('keyword'), isFalse);
    });
  });

  group('AnimeApi.create/delete/scanAnime', () {
    test(
      'create posts bangumi_id and file_path including empty string',
      () async {
        RequestOptions? seen;
        final client = clientWithHandler((options) {
          seen = options;
          return jsonBody(
            ok(<String, dynamic>{
              'id': 7,
              'title': 'Show',
              'title_cn': '番',
              'bangumi_id': 101,
              'cover': '',
              'summary': '',
              'ep_count': 12,
              'file_path': '',
              'created_at': '2026-08-20T00:00:00Z',
            }),
          );
        });
        final anime = await AnimeApi(client).create(101, '');
        expect(seen!.method, 'POST');
        expect(seen!.path, 'animes');
        expect(seen!.data, <String, dynamic>{
          'bangumi_id': 101,
          'file_path': '',
        });
        expect(anime.id, 7);
        expect(anime.filePath, '');
      },
    );

    test('delete calls DELETE /animes/:id', () async {
      RequestOptions? seen;
      final client = clientWithHandler((options) {
        seen = options;
        return jsonBody(ok(null));
      });
      await AnimeApi(client).delete(9);
      expect(seen!.method, 'DELETE');
      expect(seen!.path, 'animes/9');
    });

    test('scanAnime posts with 600s timeout', () async {
      RequestOptions? seen;
      final client = clientWithHandler((options) {
        seen = options;
        return jsonBody(
          ok(<String, dynamic>{
            'scanned': 2,
            'episodes': <Map<String, dynamic>>[
              {
                'id': 1,
                'anime_id': 9,
                'ep_number': 1,
                'title': '01',
                'file_path': 'a.mkv',
                'duration': 0,
              },
            ],
          }),
        );
      });
      final result = await AnimeApi(client).scanAnime(9);
      expect(seen!.method, 'POST');
      expect(seen!.path, 'animes/9/scan');
      expect(seen!.sendTimeout, AnimeApi.scanTimeout);
      expect(seen!.receiveTimeout, AnimeApi.scanTimeout);
      expect(result.scanned, 2);
      expect(result.episodes, hasLength(1));
    });
  });

  group('confirmUnidentified', () {
    test('creates then scans', () async {
      final calls = <String>[];
      final client = clientWithHandler((options) {
        calls.add('${options.method} ${options.path}');
        if (options.path == 'animes') {
          return jsonBody(
            ok(<String, dynamic>{
              'id': 42,
              'title': 'Show',
              'title_cn': '番剧',
              'bangumi_id': 101,
              'cover': '',
              'summary': '',
              'ep_count': 12,
              'file_path': 'ShowDir',
              'created_at': '',
            }),
          );
        }
        return jsonBody(
          ok(<String, dynamic>{'scanned': 3, 'episodes': <Object>[]}),
        );
      });
      final api = AnimeApi(client);
      final anime = await confirmUnidentified(
        animeApi: api,
        bangumiId: 101,
        filePath: 'ShowDir',
      );
      expect(anime.id, 42);
      expect(calls, ['POST animes', 'POST animes/42/scan']);
    });

    test('does not scan when create throws 1001', () async {
      final calls = <String>[];
      final client = clientWithHandler((options) {
        calls.add('${options.method} ${options.path}');
        return jsonBody(errorEnvelope(1001, '番剧已存在但目录不同'));
      });
      final api = AnimeApi(client);
      await expectLater(
        confirmUnidentified(animeApi: api, bangumiId: 101, filePath: 'Other'),
        throwsA(
          isA<ApiException>()
              .having((error) => error.code, 'code', 1001)
              .having((error) => error.message, 'message', '番剧已存在但目录不同'),
        ),
      );
      expect(calls, ['POST animes']);
    });
  });
}
