import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/api/api_client.dart';
import 'package:fan_web/api/media_api.dart';

void main() {
  group('MediaApi', () {
    test('parses media token response', () async {
      final client = _clientReturning(
        <String, dynamic>{
          'code': 0,
          'data': {'token': 'media-tok', 'expires_at': '2026-01-01T00:00:00Z'},
        },
      );
      final result = await MediaApi(client).fetchMediaToken(7);
      expect(result.token, 'media-tok');
      expect(result.expiresAt, '2026-01-01T00:00:00Z');
    });

    test('throws MediaTokenUnsupported on business code 404', () async {
      final client = _clientReturning(
        <String, dynamic>{'code': 404, 'message': '接口不存在', 'data': null},
      );
      expect(
        () => MediaApi(client).fetchMediaToken(7),
        throwsA(isA<MediaTokenUnsupported>()),
      );
    });

    test('does not fall back on 401', () async {
      final client = _clientReturning(
        <String, dynamic>{'code': 2001, 'message': '未登录', 'data': null},
      );
      expect(
        () => MediaApi(client).fetchMediaToken(7),
        throwsA(isA<ApiException>()),
      );
    });

    test('URL builders encode tokens', () {
      expect(
        buildStreamUrlWithMediaToken(' http://127.0.0.1:8080/// ', 42, 'token+/='),
        'http://127.0.0.1:8080/api/episodes/42/stream?media_token=token%2B%2F%3D',
      );
      expect(
        buildLegacyStreamUrl('http://127.0.0.1:8080', 42, 'jwt+/token'),
        'http://127.0.0.1:8080/api/episodes/42/stream?token=jwt%2B%2Ftoken',
      );
    });
  });
}

ApiClient _clientReturning(Map<String, dynamic> envelope) {
  final dio = Dio();
  dio.httpClientAdapter = _StubAdapter(envelope);
  return ApiClient(dio: dio);
}

class _StubAdapter implements HttpClientAdapter {
  _StubAdapter(this.envelope);

  final Map<String, dynamic> envelope;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    return ResponseBody.fromString(
      jsonEncode(envelope),
      200,
      headers: <String, List<String>>{
        Headers.contentTypeHeader: <String>['application/json'],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}