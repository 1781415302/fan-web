import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/api/anime_api.dart';

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
}
