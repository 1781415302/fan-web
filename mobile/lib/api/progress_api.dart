import 'package:dio/dio.dart';

import '../models/anime.dart';
import 'api_client.dart';

class ProgressApi {
  ProgressApi(this._client);

  final ApiClient _client;

  Future<AnimeProgress> getAnimeProgress(int animeId) {
    return _request(() async {
      final response = await _client.dio.get<dynamic>(
        'progress/anime/$animeId',
      );
      final data = response.data;
      if (data is! List) {
        throw const FormatException('番剧进度格式错误');
      }
      return data
          .whereType<Map>()
          .map(
            (item) => EpisodeProgress.fromJson(Map<String, dynamic>.from(item)),
          )
          .toList(growable: false);
    });
  }

  Future<EpisodeProgress> getEpisodeProgress(int episodeId) {
    return _request(() async {
      final response = await _client.dio.get<dynamic>('progress/$episodeId');
      final data = response.data;
      if (data is! Map) {
        throw const FormatException('单集进度格式错误');
      }
      return EpisodeProgress.fromJson({
        ...Map<String, dynamic>.from(data),
        'episode_id': episodeId,
      });
    });
  }

  Future<void> reportProgress(int episodeId, int position, bool watched) {
    return _request(() async {
      await _client.dio.post<dynamic>(
        'progress/$episodeId',
        data: <String, dynamic>{'position': position, 'watched': watched},
      );
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
}
