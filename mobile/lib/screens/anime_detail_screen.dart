import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../models/anime.dart';
import '../providers/anime_provider.dart';
import '../providers/player_provider.dart';
import '../theme/app_theme.dart';
import '../widgets/anime_card.dart';
import '../widgets/episode_tile.dart';
import '../widgets/state_widgets.dart';

class AnimeDetailScreen extends ConsumerStatefulWidget {
  const AnimeDetailScreen({required this.animeId, super.key});

  final int animeId;

  @override
  ConsumerState<AnimeDetailScreen> createState() => _AnimeDetailScreenState();
}

class _AnimeDetailScreenState extends ConsumerState<AnimeDetailScreen> {
  Anime? _anime;
  List<Episode> _episodes = const [];
  Map<int, EpisodeProgress> _progressByEpisode = const {};
  String? _error;
  String? _refreshError;
  bool _isLoading = true;
  bool _summaryExpanded = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        unawaited(_load());
      }
    });
  }

  Future<void> _load() async {
    if (mounted) {
      setState(() {
        _isLoading = true;
        _error = null;
        _refreshError = null;
      });
    }

    try {
      final results = await Future.wait<Object>([
        ref.read(animeApiProvider).getById(widget.animeId),
        ref.read(animeApiProvider).listEpisodes(widget.animeId),
        ref.read(progressApiProvider).getAnimeProgress(widget.animeId),
      ]);
      if (!mounted) {
        return;
      }
      final anime = results[0] as Anime;
      final episodes = results[1] as List<Episode>;
      final progress = results[2] as AnimeProgress;
      setState(() {
        _anime = anime;
        _episodes = episodes;
        _progressByEpisode = {
          for (final item in progress) item.episodeId: item,
        };
        _isLoading = false;
      });
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _isLoading = false;
        _error = apiErrorMessage(error);
      });
    }
  }

  /// 加载番剧主体成功后，单独刷新集数和进度（用于从播放器返回后仅刷新进度）。
  Future<void> _refreshProgress() async {
    try {
      final results = await Future.wait<Object>([
        ref.read(animeApiProvider).listEpisodes(widget.animeId),
        ref.read(progressApiProvider).getAnimeProgress(widget.animeId),
      ]);
      if (!mounted) {
        return;
      }
      final episodes = results[0] as List<Episode>;
      final progress = results[1] as AnimeProgress;
      setState(() {
        _episodes = episodes;
        _progressByEpisode = {
          for (final item in progress) item.episodeId: item,
        };
        _refreshError = null;
      });
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _refreshError = apiErrorMessage(error);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final anime = _anime;
    return Scaffold(
      appBar: AppBar(
        title: Text(
          anime == null ? '番剧详情' : _displayTitle(anime),
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
      ),
      body: _isLoading && anime == null
          ? const Center(child: CircularProgressIndicator())
          : _error != null && anime == null
          ? ErrorStateView(message: _error!, onRetry: _load)
          : _buildContent(anime!),
    );
  }

  Widget _buildContent(Anime anime) {
    final progressTotal = anime.epCount > 0 ? anime.epCount : _episodes.length;
    final watchedCount = _progressByEpisode.values
        .where((progress) => progress.watched)
        .length;
    final progressPercent = progressTotal <= 0
        ? 0
        : ((watchedCount / progressTotal) * 100).round().clamp(0, 100).toInt();
    final continueEpisode = _continueEpisode;

    return RefreshIndicator(
      onRefresh: _load,
      color: AppTheme.accent,
      backgroundColor: AppTheme.muted,
      child: ListView(
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: [
          _DetailHeader(
            anime: anime,
            watchedCount: watchedCount,
            progressTotal: progressTotal,
            progressPercent: progressPercent,
            continueEpisode: continueEpisode,
            onContinue: continueEpisode == null
                ? null
                : () => unawaited(_openPlayer(continueEpisode)),
          ),
          const SizedBox(height: 28),
          _buildSummary(anime),
          const SizedBox(height: 28),
          _buildEpisodes(),
          if (_refreshError != null) ...[
            const SizedBox(height: 12),
            Text(
              '进度刷新失败：$_refreshError',
              textAlign: TextAlign.center,
              style: const TextStyle(color: AppTheme.warning, fontSize: 13),
            ),
          ],
        ],
      ),
    );
  }

  Widget _buildSummary(Anime anime) {
    final summary = anime.summary.trim();
    final canCollapse = summary.length > 180;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const _SectionHeading(title: '简介'),
        const SizedBox(height: 10),
        Text(
          summary.isEmpty ? '暂无简介' : summary,
          maxLines: canCollapse && !_summaryExpanded ? 4 : null,
          overflow: canCollapse && !_summaryExpanded
              ? TextOverflow.ellipsis
              : TextOverflow.visible,
          style: TextStyle(
            color: AppTheme.foreground.withValues(alpha: 0.78),
            height: 1.6,
          ),
        ),
        if (canCollapse)
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton(
              onPressed: () {
                setState(() {
                  _summaryExpanded = !_summaryExpanded;
                });
              },
              child: Text(_summaryExpanded ? '收起' : '展开'),
            ),
          ),
      ],
    );
  }

  Widget _buildEpisodes() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _SectionHeading(title: '集数', trailing: '共${_episodes.length}集'),
        const SizedBox(height: 12),
        if (_episodes.isEmpty)
          Text(
            '暂无集数',
            style: TextStyle(
              color: AppTheme.foreground.withValues(alpha: 0.65),
            ),
          )
        else
          LayoutBuilder(
            builder: (context, constraints) {
              final columnCount = constraints.maxWidth >= 600 ? 5 : 3;
              final tileWidth =
                  (constraints.maxWidth - (columnCount - 1) * 10) / columnCount;
              return Wrap(
                spacing: 10,
                runSpacing: 10,
                children: [
                  for (final episode in _episodes)
                    SizedBox(
                      width: tileWidth,
                      height: 78,
                      child: EpisodeTile(
                        episode: episode,
                        progress: _progressByEpisode[episode.id],
                        onTap: () => unawaited(_openPlayer(episode)),
                      ),
                    ),
                ],
              );
            },
          ),
      ],
    );
  }

  Episode? get _continueEpisode {
    return pickContinueEpisode(_episodes, _progressByEpisode);
  }

  Future<void> _openPlayer(Episode episode) async {
    final anime = _anime;
    if (anime == null) {
      return;
    }
    await context.pushNamed(
      'watch',
      pathParameters: {'id': '${widget.animeId}', 'epId': '${episode.id}'},
      extra: PlayerLaunchInfo(
        animeTitle: _displayTitle(anime),
        episodeNumber: episode.epNumber,
      ),
    );
    if (mounted) {
      await _refreshProgress();
    }
  }

  static String _displayTitle(Anime anime) {
    final titleCn = anime.titleCn.trim();
    final title = anime.title.trim();
    if (titleCn.isNotEmpty) {
      return titleCn;
    }
    return title.isEmpty ? '未命名番剧' : title;
  }
}

class _DetailHeader extends StatelessWidget {
  const _DetailHeader({
    required this.anime,
    required this.watchedCount,
    required this.progressTotal,
    required this.progressPercent,
    required this.continueEpisode,
    required this.onContinue,
  });

  final Anime anime;
  final int watchedCount;
  final int progressTotal;
  final int progressPercent;
  final Episode? continueEpisode;
  final VoidCallback? onContinue;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final compact = constraints.maxWidth < 360;
        final cover = SizedBox(
          width: compact ? 140 : 128,
          child: AspectRatio(
            aspectRatio: 2 / 3,
            child: AnimeCover(
              animeId: anime.id,
              cover: anime.cover,
              label: '${_displayTitle(anime)}封面',
              borderRadius: BorderRadius.circular(12),
            ),
          ),
        );
        final details = _HeaderDetails(
          anime: anime,
          watchedCount: watchedCount,
          progressTotal: progressTotal,
          progressPercent: progressPercent,
          continueEpisode: continueEpisode,
          onContinue: onContinue,
        );

        if (compact) {
          return Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [cover, const SizedBox(height: 16), details],
          );
        }
        return Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            cover,
            Expanded(
              child: Padding(
                padding: const EdgeInsets.only(left: 16),
                child: details,
              ),
            ),
          ],
        );
      },
    );
  }

  static String _displayTitle(Anime anime) {
    final titleCn = anime.titleCn.trim();
    final title = anime.title.trim();
    return titleCn.isNotEmpty ? titleCn : (title.isEmpty ? '未命名番剧' : title);
  }
}

class _HeaderDetails extends StatelessWidget {
  const _HeaderDetails({
    required this.anime,
    required this.watchedCount,
    required this.progressTotal,
    required this.progressPercent,
    required this.continueEpisode,
    required this.onContinue,
  });

  final Anime anime;
  final int watchedCount;
  final int progressTotal;
  final int progressPercent;
  final Episode? continueEpisode;
  final VoidCallback? onContinue;

  @override
  Widget build(BuildContext context) {
    final titleCn = anime.titleCn.trim();
    final title = anime.title.trim();
    final showOriginal = titleCn.isNotEmpty && title.isNotEmpty;
    final episodeCount = anime.epCount > 0 ? '全${anime.epCount}话' : '集数未知';

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          titleCn.isNotEmpty ? titleCn : (title.isEmpty ? '未命名番剧' : title),
          maxLines: 3,
          overflow: TextOverflow.ellipsis,
          style: const TextStyle(
            color: AppTheme.foreground,
            fontSize: 22,
            fontWeight: FontWeight.w700,
            height: 1.25,
          ),
        ),
        if (showOriginal) ...[
          const SizedBox(height: 6),
          Text(
            title,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(
              color: AppTheme.foreground.withValues(alpha: 0.62),
              height: 1.35,
            ),
          ),
        ],
        const SizedBox(height: 14),
        Text(
          episodeCount,
          style: TextStyle(
            color: AppTheme.foreground.withValues(alpha: 0.72),
            fontSize: 13,
          ),
        ),
        const SizedBox(height: 14),
        Row(
          mainAxisAlignment: MainAxisAlignment.spaceBetween,
          children: [
            Text(
              '$watchedCount / $progressTotal 已看',
              style: const TextStyle(
                color: AppTheme.accent,
                fontWeight: FontWeight.w600,
              ),
            ),
            Text(
              '$progressPercent%',
              style: const TextStyle(
                color: AppTheme.accent,
                fontWeight: FontWeight.w600,
              ),
            ),
          ],
        ),
        const SizedBox(height: 7),
        Semantics(
          label: '观看进度 $progressPercent%',
          child: ClipRRect(
            borderRadius: BorderRadius.circular(999),
            child: LinearProgressIndicator(
              value: progressPercent / 100,
              minHeight: 6,
              backgroundColor: AppTheme.border,
              valueColor: const AlwaysStoppedAnimation<Color>(AppTheme.accent),
            ),
          ),
        ),
        if (onContinue != null && continueEpisode != null) ...[
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: onContinue,
            icon: const Icon(Icons.play_arrow),
            label: Text('从第${continueEpisode!.epNumber}话继续'),
          ),
        ],
      ],
    );
  }
}

class _SectionHeading extends StatelessWidget {
  const _SectionHeading({required this.title, this.trailing});

  final String title;
  final String? trailing;

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.baseline,
      textBaseline: TextBaseline.alphabetic,
      children: [
        Text(
          title,
          style: const TextStyle(
            color: AppTheme.foreground,
            fontSize: 18,
            fontWeight: FontWeight.w700,
          ),
        ),
        if (trailing != null) ...[
          const SizedBox(width: 10),
          Text(
            trailing!,
            style: TextStyle(
              color: AppTheme.foreground.withValues(alpha: 0.58),
              fontSize: 13,
            ),
          ),
        ],
      ],
    );
  }
}
