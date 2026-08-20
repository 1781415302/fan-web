import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/auth_provider.dart';
import 'api_client.dart';

final bangumiApiProvider = Provider<BangumiApi>((ref) {
  return BangumiApi(ref.watch(apiClientProvider));
});

class BangumiApi {
  BangumiApi(this._client);

  final ApiClient _client;

  Future<List<BangumiSearchItem>> search(String keyword) {
    return _request(() async {
      final response = await _client.dio.get<dynamic>(
        'bangumi/search',
        queryParameters: <String, dynamic>{'keyword': keyword},
      );
      final data = response.data;
      if (data is! List) {
        throw const FormatException('Bangumi 搜索格式错误');
      }
      return data
          .whereType<Map>()
          .map(
            (item) =>
                BangumiSearchItem.fromJson(Map<String, dynamic>.from(item)),
          )
          .toList(growable: false);
    });
  }

  Future<BangumiSubject> getSubject(int id) {
    return _request(() async {
      final response = await _client.dio.get<dynamic>('bangumi/subject/$id');
      return BangumiSubject.fromJson(_asJsonMap(response.data, 'Bangumi 条目'));
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

class BangumiSearchItem {
  const BangumiSearchItem({
    required this.id,
    required this.name,
    required this.nameCn,
    required this.summary,
    required this.epsCount,
    required this.cover,
  });

  factory BangumiSearchItem.fromJson(Map<String, dynamic> json) {
    return BangumiSearchItem(
      id: _asInt(json['id']),
      name: _asString(json['name']),
      nameCn: _asString(json['name_cn']),
      summary: _asString(json['summary']),
      epsCount: _asInt(json['eps_count']),
      cover: _asString(json['cover']),
    );
  }

  final int id;
  final String name;
  final String nameCn;
  final String summary;
  final int epsCount;
  final String cover;

  String get displayName {
    final cn = nameCn.trim();
    return cn.isNotEmpty ? cn : (name.isEmpty ? '未命名番剧' : name);
  }
}

class BangumiSubject {
  const BangumiSubject({
    required this.id,
    required this.name,
    required this.nameCn,
    required this.summary,
    required this.cover,
    required this.totalEpisodes,
  });

  factory BangumiSubject.fromJson(Map<String, dynamic> json) {
    return BangumiSubject(
      id: _asInt(json['id']),
      name: _asString(json['name']),
      nameCn: _asString(json['name_cn']),
      summary: _asString(json['summary']),
      cover: _asString(json['cover']),
      totalEpisodes: _asInt(json['total_episodes']),
    );
  }

  final int id;
  final String name;
  final String nameCn;
  final String summary;
  final String cover;
  final int totalEpisodes;
}

int _asInt(Object? value) {
  if (value is num) {
    return value.toInt();
  }
  return int.tryParse(value?.toString() ?? '') ?? 0;
}

String _asString(Object? value) => value?.toString() ?? '';
