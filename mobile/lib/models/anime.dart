int _asInt(Object? value) {
  if (value is num) {
    return value.toInt();
  }
  return int.tryParse(value?.toString() ?? '') ?? 0;
}

String _asString(Object? value) => value?.toString() ?? '';

class Anime {
  const Anime({
    required this.id,
    required this.title,
    required this.titleCn,
    required this.bangumiId,
    required this.cover,
    required this.summary,
    required this.epCount,
    required this.filePath,
    required this.createdAt,
  });

  factory Anime.fromJson(Map<String, dynamic> json) {
    return Anime(
      id: _asInt(json['id']),
      title: _asString(json['title']),
      titleCn: _asString(json['title_cn']),
      bangumiId: _asInt(json['bangumi_id']),
      cover: _asString(json['cover']),
      summary: _asString(json['summary']),
      epCount: _asInt(json['ep_count']),
      filePath: _asString(json['file_path']),
      createdAt: _asString(json['created_at']),
    );
  }

  final int id;
  final String title;
  final String titleCn;
  final int bangumiId;
  final String cover;
  final String summary;
  final int epCount;
  final String filePath;
  final String createdAt;

  Map<String, dynamic> toJson() => {
        'id': id,
        'title': title,
        'title_cn': titleCn,
        'bangumi_id': bangumiId,
        'cover': cover,
        'summary': summary,
        'ep_count': epCount,
        'file_path': filePath,
        'created_at': createdAt,
      };
}

class AnimeListItem {
  const AnimeListItem({required this.anime, required this.watchedCount});

  factory AnimeListItem.fromJson(Map<String, dynamic> json) {
    return AnimeListItem(
      anime: Anime.fromJson(json),
      watchedCount: _asInt(json['watched_count']),
    );
  }

  final Anime anime;
  final int watchedCount;

  Map<String, dynamic> toJson() => {
        ...anime.toJson(),
        'watched_count': watchedCount,
      };
}

class Episode {
  const Episode({
    required this.id,
    required this.animeId,
    required this.epNumber,
    required this.title,
    required this.filePath,
    required this.duration,
  });

  factory Episode.fromJson(Map<String, dynamic> json) {
    return Episode(
      id: _asInt(json['id']),
      animeId: _asInt(json['anime_id']),
      epNumber: _asInt(json['ep_number']),
      title: _asString(json['title']),
      filePath: _asString(json['file_path']),
      duration: _asInt(json['duration']),
    );
  }

  final int id;
  final int animeId;
  final int epNumber;
  final String title;
  final String filePath;
  final int duration;
}

class PaginatedAnimes {
  const PaginatedAnimes({
    required this.items,
    required this.total,
    required this.page,
    required this.pageSize,
  });

  factory PaginatedAnimes.fromJson(Map<String, dynamic> json) {
    final rawItems = json['items'];
    final items = rawItems is List
        ? rawItems
              .whereType<Map>()
              .map(
                (item) =>
                    AnimeListItem.fromJson(Map<String, dynamic>.from(item)),
              )
              .toList(growable: false)
        : const <AnimeListItem>[];

    return PaginatedAnimes(
      items: items,
      total: _asInt(json['total']),
      page: _asInt(json['page']),
      pageSize: _asInt(json['page_size']),
    );
  }

  final List<AnimeListItem> items;
  final int total;
  final int page;
  final int pageSize;
}

class EpisodeProgress {
  const EpisodeProgress({
    required this.episodeId,
    required this.position,
    required this.watched,
    required this.updatedAt,
  });

  factory EpisodeProgress.fromJson(Map<String, dynamic> json) {
    return EpisodeProgress(
      episodeId: _asInt(json['episode_id']),
      position: _asInt(json['position']),
      watched: json['watched'] == true || _asInt(json['watched']) == 1,
      updatedAt: _asString(json['updated_at']),
    );
  }

  final int episodeId;
  final int position;
  final bool watched;
  final String updatedAt;
}

// The endpoint returns an array, so this alias keeps the API response type
// explicit without introducing a wrapper that does not exist on the server.
typedef AnimeProgress = List<EpisodeProgress>;

/// 选择"继续播放"的集数：优先第一个"进行中"（!watched && position>0），
/// 没有则第一个"未看"（!watched），全部已看返回 null。
Episode? pickContinueEpisode(
  List<Episode> episodes,
  Map<int, EpisodeProgress> progressByEpisode,
) {
  for (final episode in episodes) {
    final progress = progressByEpisode[episode.id];
    if (progress != null && !progress.watched && progress.position > 0) {
      return episode;
    }
  }
  for (final episode in episodes) {
    final progress = progressByEpisode[episode.id];
    if (progress == null || !progress.watched) {
      return episode;
    }
  }
  return null;
}
