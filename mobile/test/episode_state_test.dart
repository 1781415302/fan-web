import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/models/anime.dart';
import 'package:fan_web/widgets/episode_tile.dart';

void main() {
  group('EpisodeTile.statusOf', () {
    test('null progress is unwatched', () {
      expect(EpisodeTile.statusOf(null), EpisodeStatus.unwatched);
    });

    test('watched true is watched regardless of position', () {
      expect(
        EpisodeTile.statusOf(
          const EpisodeProgress(
            episodeId: 1,
            position: 0,
            watched: true,
            updatedAt: '',
          ),
        ),
        EpisodeStatus.watched,
      );
    });

    test('position > 0 and not watched is inProgress', () {
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
    });

    test('position 0 and not watched is unwatched', () {
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
    });
  });

  group('pickContinueEpisode', () {
    final ep1 = const Episode(
      id: 1,
      animeId: 1,
      epNumber: 1,
      title: '',
      filePath: '',
      duration: 0,
    );
    final ep2 = const Episode(
      id: 2,
      animeId: 1,
      epNumber: 2,
      title: '',
      filePath: '',
      duration: 0,
    );
    final ep3 = const Episode(
      id: 3,
      animeId: 1,
      epNumber: 3,
      title: '',
      filePath: '',
      duration: 0,
    );

    test('prefers in-progress over unwatched', () {
      final result = pickContinueEpisode([ep1, ep2, ep3], <int, EpisodeProgress>{
        1: const EpisodeProgress(
          episodeId: 1,
          position: 100,
          watched: true,
          updatedAt: '',
        ),
        2: const EpisodeProgress(
          episodeId: 2,
          position: 30,
          watched: false,
          updatedAt: '',
        ),
      });
      expect(result?.id, 2);
    });

    test('falls back to first unwatched', () {
      final result = pickContinueEpisode([ep1, ep2, ep3], <int, EpisodeProgress>{
        1: const EpisodeProgress(
          episodeId: 1,
          position: 100,
          watched: true,
          updatedAt: '',
        ),
      });
      expect(result?.id, 2);
    });

    test('returns null when all watched', () {
      final result = pickContinueEpisode([ep1, ep2], <int, EpisodeProgress>{
        1: const EpisodeProgress(
          episodeId: 1,
          position: 100,
          watched: true,
          updatedAt: '',
        ),
        2: const EpisodeProgress(
          episodeId: 2,
          position: 100,
          watched: true,
          updatedAt: '',
        ),
      });
      expect(result, isNull);
    });

    test('returns null for empty list', () {
      expect(pickContinueEpisode([], {}), isNull);
    });

    test('returns first episode when no progress at all', () {
      final result = pickContinueEpisode([ep1, ep2], <int, EpisodeProgress>{});
      expect(result?.id, 1);
    });
  });
}
