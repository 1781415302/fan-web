import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/api/bangumi_api.dart';

import 'http_stub.dart';

void main() {
  group('BangumiApi', () {
    test('search sends keyword and parses items', () async {
      RequestOptions? seen;
      final client = clientWithHandler((options) {
        seen = options;
        return jsonBody(
          ok(<Map<String, dynamic>>[
            {
              'id': 8,
              'name': 'Show',
              'name_cn': '中文',
              'summary': '简介',
              'eps_count': 12,
              'cover': 'https://example/cover.jpg',
            },
          ]),
        );
      });
      final items = await BangumiApi(client).search('关键词');
      expect(seen!.method, 'GET');
      expect(seen!.path, 'bangumi/search');
      expect(seen!.queryParameters['keyword'], '关键词');
      expect(items.single.displayName, '中文');
      expect(items.single.epsCount, 12);
    });

    test('getSubject fetches /bangumi/subject/:id', () async {
      RequestOptions? seen;
      final client = clientWithHandler((options) {
        seen = options;
        return jsonBody(
          ok(<String, dynamic>{
            'id': 8,
            'name': 'Show',
            'name_cn': '中文',
            'summary': '简介',
            'cover': '',
            'total_episodes': 24,
          }),
        );
      });
      final subject = await BangumiApi(client).getSubject(8);
      expect(seen!.path, 'bangumi/subject/8');
      expect(subject.totalEpisodes, 24);
    });
  });
}
