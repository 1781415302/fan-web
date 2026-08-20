import 'package:dio/dio.dart';

import '../models/anime.dart';
import 'api_client.dart';

class AnimeApi {
  AnimeApi(this._client);

  final ApiClient _client;

  static const scanTimeout = Duration(seconds: 600);

  Future<PaginatedAnimes> list({
    int page = 1,
    int pageSize = 20,
    String? keyword,
  }) {
    return _request(() async {
      final query = <String, dynamic>{'page': page, 'page_size': pageSize};
      final trimmed = keyword?.trim();
      if (trimmed != null && trimmed.isNotEmpty) {
        query['keyword'] = trimmed;
      }
      final response = await _client.dio.get<dynamic>(
        'animes',
        queryParameters: query,
      );
      return PaginatedAnimes.fromJson(_asJsonMap(response.data, '番剧列表'));
    });
  }

  Future<Anime> getById(int id) {
    return _request(() async {
      final response = await _client.dio.get<dynamic>('animes/$id');
      return Anime.fromJson(_asJsonMap(response.data, '番剧详情'));
    });
  }

  Future<List<Episode>> listEpisodes(int animeId) {
    return _request(() async {
      final response = await _client.dio.get<dynamic>(
        'animes/$animeId/episodes',
      );
      final data = response.data;
      if (data is! List) {
        throw const FormatException('集数列表格式错误');
      }
      return data
          .whereType<Map>()
          .map((item) => Episode.fromJson(Map<String, dynamic>.from(item)))
          .toList(growable: false);
    });
  }

  Future<Anime> create(int bangumiId, String filePath) {
    return _request(() async {
      final response = await _client.dio.post<dynamic>(
        'animes',
        data: <String, dynamic>{'bangumi_id': bangumiId, 'file_path': filePath},
      );
      return Anime.fromJson(_asJsonMap(response.data, '创建番剧'));
    });
  }

  Future<void> delete(int id) {
    return _request(() async {
      await _client.dio.delete<dynamic>('animes/$id');
    });
  }

  Future<AnimeScanResult> scanAnime(int id) {
    return _request(() async {
      final response = await _client.dio.post<dynamic>(
        'animes/$id/scan',
        options: Options(sendTimeout: scanTimeout, receiveTimeout: scanTimeout),
      );
      return AnimeScanResult.fromJson(_asJsonMap(response.data, '番剧扫描'));
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

class AnimeScanResult {
  const AnimeScanResult({required this.scanned, required this.episodes});

  factory AnimeScanResult.fromJson(Map<String, dynamic> json) {
    final raw = json['episodes'];
    final episodes = raw is List
        ? raw
              .whereType<Map>()
              .map((item) => Episode.fromJson(Map<String, dynamic>.from(item)))
              .toList(growable: false)
        : const <Episode>[];
    return AnimeScanResult(
      scanned: _asInt(json['scanned']),
      episodes: episodes,
    );
  }

  final int scanned;
  final List<Episode> episodes;
}

/// 确认未识别：先 create 再 scan。Create 抛错（含 1001）时不会调用 scan。
Future<Anime> confirmUnidentified({
  required AnimeApi animeApi,
  required int bangumiId,
  required String filePath,
}) async {
  final anime = await animeApi.create(bangumiId, filePath);
  await animeApi.scanAnime(anime.id);
  return anime;
}

int _asInt(Object? value) {
  if (value is num) {
    return value.toInt();
  }
  return int.tryParse(value?.toString() ?? '') ?? 0;
}
