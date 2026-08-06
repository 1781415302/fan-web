import 'package:dio/dio.dart';

import '../models/anime.dart';
import 'api_client.dart';

class AnimeApi {
  AnimeApi(this._client);

  final ApiClient _client;

  Future<PaginatedAnimes> list({int page = 1, int pageSize = 20}) {
    return _request(() async {
      final response = await _client.dio.get<dynamic>(
        'animes',
        queryParameters: <String, dynamic>{'page': page, 'page_size': pageSize},
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
