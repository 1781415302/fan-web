import 'package:dio/dio.dart';

import 'api_client.dart';

class BangumiLink {
  const BangumiLink({required this.linked, this.suffix});

  final bool linked;
  final String? suffix;

  factory BangumiLink.fromJson(Map<String, dynamic> json) {
    final linked = json['linked'] == true;
    final raw = json['suffix']?.toString();
    final suffix = (raw == null || raw.isEmpty) ? null : raw;
    return BangumiLink(linked: linked, suffix: linked ? suffix : null);
  }
}

class BangumiSyncResult {
  const BangumiSyncResult({
    required this.animes,
    required this.episodesMarked,
  });

  final int animes;
  final int episodesMarked;

  factory BangumiSyncResult.fromJson(Map<String, dynamic> json) {
    return BangumiSyncResult(
      animes: _asInt(json['animes']),
      episodesMarked: _asInt(json['episodes_marked']),
    );
  }
}

int _asInt(Object? value) {
  if (value is int) {
    return value;
  }
  if (value is num) {
    return value.toInt();
  }
  return 0;
}

class BangumiMeApi {
  BangumiMeApi(this._client);

  final ApiClient _client;

  static const syncTimeout = Duration(seconds: 120);

  Future<BangumiLink> getLink() {
    return _request(() async {
      final response = await _client.dio.get<dynamic>('me/bangumi');
      return BangumiLink.fromJson(_asJsonMap(response.data, 'Bangumi 绑定'));
    });
  }

  Future<BangumiLink> putToken(String accessToken) {
    return _request(() async {
      final response = await _client.dio.put<dynamic>(
        'me/bangumi',
        data: <String, dynamic>{'access_token': accessToken},
      );
      return BangumiLink.fromJson(_asJsonMap(response.data, 'Bangumi 绑定'));
    });
  }

  Future<BangumiLink> deleteToken() {
    return _request(() async {
      final response = await _client.dio.delete<dynamic>('me/bangumi');
      return BangumiLink.fromJson(_asJsonMap(response.data, 'Bangumi 绑定'));
    });
  }

  /// 入站同步单独 120s，禁止走 ApiClient 默认 10s。
  Future<BangumiSyncResult> sync() {
    return _request(() async {
      final response = await _client.dio.post<dynamic>(
        'me/bangumi/sync',
        options: Options(
          sendTimeout: syncTimeout,
          receiveTimeout: syncTimeout,
        ),
      );
      return BangumiSyncResult.fromJson(_asJsonMap(response.data, '同步结果'));
    });
  }

  Future<T> _request<T>(Future<T> Function() request) async {
    try {
      return await request();
    } on DioException catch (error) {
      final apiError = error.error;
      if (apiError is ApiException) {
        throw apiError;
      }
      rethrow;
    }
  }

  Map<String, dynamic> _asJsonMap(Object? value, String label) {
    if (value is Map) {
      return Map<String, dynamic>.from(value);
    }
    throw FormatException('$label格式错误');
  }
}
