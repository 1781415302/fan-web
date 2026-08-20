import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/api/progress_api.dart';
import 'package:fan_web/models/anime.dart';
import 'package:fan_web/widgets/episode_tile.dart';

void main() {
  test('parses a paginated anime response with flattened watched count', () {
    final result = PaginatedAnimes.fromJson({
      'items': [
        {
          'id': 1,
          'title': 'Original title',
          'title_cn': '中文标题',
          'bangumi_id': 1000,
          'cover': '',
          'summary': '简介',
          'ep_count': 12,
          'file_path': 'show',
          'created_at': '2026-08-01T10:00:00Z',
          'watched_count': 5,
        },
      ],
      'total': 42,
      'page': 1,
      'page_size': 20,
    });

    expect(result.total, 42);
    expect(result.items, hasLength(1));
    expect(result.items.single.anime.titleCn, '中文标题');
    expect(result.items.single.watchedCount, 5);
  });

  test('parses progress boolean and numeric values', () {
    final watched = EpisodeProgress.fromJson({
      'episode_id': 10,
      'position': 360,
      'watched': true,
      'updated_at': '2026-08-05T20:30:00Z',
    });
    final inProgress = EpisodeProgress.fromJson({
      'episode_id': 11,
      'position': 12,
      'watched': 0,
      'updated_at': '',
    });

    expect(watched.watched, isTrue);
    expect(watched.position, 360);
    expect(inProgress.watched, isFalse);
    expect(inProgress.position, 12);
  });

  test('episode status follows watched before position', () {
    expect(EpisodeTile.statusOf(null), EpisodeStatus.unwatched);
    expect(
      EpisodeTile.statusOf(
        const EpisodeProgress(
          episodeId: 1,
          position: 0,
          watched: false,
          updatedAt: '',
        ),
      ),
      EpisodeStatus.unwatched,
    );
    expect(
      EpisodeTile.statusOf(
        const EpisodeProgress(
          episodeId: 1,
          position: 20,
          watched: false,
          updatedAt: '',
        ),
      ),
      EpisodeStatus.inProgress,
    );
    expect(
      EpisodeTile.statusOf(
        const EpisodeProgress(
          episodeId: 1,
          position: 20,
          watched: true,
          updatedAt: '',
        ),
      ),
      EpisodeStatus.watched,
    );
  });

  test('parses continue API items envelope', () {
    final items = parseContinueItems({
      'items': [
        {
          'anime': {
            'id': 3,
            'title': 'Original',
            'title_cn': '中文标题',
            'bangumi_id': 9,
            'cover': '',
            'summary': '',
            'ep_count': 12,
            'file_path': 'show',
            'created_at': '2026-08-01T10:00:00Z',
          },
          'episode': {
            'id': 21,
            'anime_id': 3,
            'ep_number': 2,
            'title': '02',
            'file_path': '02.mkv',
            'duration': 1440,
          },
          'position': 125,
          'watched': 0,
          'updated_at': '2026-08-20T08:00:00Z',
        },
      ],
    });

    expect(items, hasLength(1));
    expect(items.single.anime.titleCn, '中文标题');
    expect(items.single.episode.id, 21);
    expect(items.single.episode.epNumber, 2);
    expect(items.single.position, 125);
    expect(items.single.watched, isFalse);
  });

  test('continue items stay empty when API returns no items', () {
    expect(parseContinueItems({'items': []}), isEmpty);
    expect(parseContinueItems({'items': null}), isEmpty);
  });
}
