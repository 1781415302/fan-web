import 'package:dio/dio.dart';

import '../models/anime.dart';
import 'api_client.dart';

class ContinueItem {
  const ContinueItem({
    required this.anime,
    required this.episode,
    required this.position,
    required this.watched,
    required this.updatedAt,
  });

  factory ContinueItem.fromJson(Map<String, dynamic> json) {
    final animeRaw = json['anime'];
    final episodeRaw = json['episode'];
    if (animeRaw is! Map || episodeRaw is! Map) {
      throw const FormatException('继续观看格式错误');
    }
    return ContinueItem(
      anime: Anime.fromJson(Map<String, dynamic>.from(animeRaw)),
      episode: Episode.fromJson(Map<String, dynamic>.from(episodeRaw)),
      position: _asInt(json['position']),
      watched: json['watched'] == true || _asInt(json['watched']) == 1,
      updatedAt: _asString(json['updated_at']),
    );
  }

  final Anime anime;
  final Episode episode;
  final int position;
  final bool watched;
  final String updatedAt;
}

List<ContinueItem> parseContinueItems(Object? data) {
  if (data is! Map) {
    throw const FormatException('继续观看格式错误');
  }
  final rawItems = data['items'];
  if (rawItems == null) {
    return const [];
  }
  if (rawItems is! List) {
    throw const FormatException('继续观看格式错误');
  }
  return rawItems
      .whereType<Map>()
      .map((item) => ContinueItem.fromJson(Map<String, dynamic>.from(item)))
      .toList(growable: false);
}

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

  Future<List<ContinueItem>> listContinue() {
    return _request(() async {
      final response = await _client.dio.get<dynamic>('progress/continue');
      return parseContinueItems(response.data);
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

int _asInt(Object? value) {
  if (value is num) {
    return value.toInt();
  }
  return int.tryParse(value?.toString() ?? '') ?? 0;
}

String _asString(Object? value) => value?.toString() ?? '';
