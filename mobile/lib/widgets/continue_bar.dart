import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../api/progress_api.dart';
import '../models/anime.dart';
import '../providers/anime_provider.dart';
import '../providers/auth_provider.dart';
import '../providers/player_provider.dart';
import '../theme/app_theme.dart';
import 'anime_card.dart';

class ContinueBar extends ConsumerStatefulWidget {
  const ContinueBar({super.key});

  @override
  ConsumerState<ContinueBar> createState() => _ContinueBarState();
}

class _ContinueBarState extends ConsumerState<ContinueBar> {
  List<ContinueItem> _items = const [];
  bool _loading = true;

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
    final client = ref.read(apiClientProvider);
    if (client.baseUrl == null || client.baseUrl!.isEmpty) {
      if (!mounted) {
        return;
      }
      setState(() {
        _items = const [];
        _loading = false;
      });
      return;
    }
    try {
      final items = await ref.read(progressApiProvider).listContinue();
      if (!mounted) {
        return;
      }
      setState(() {
        _items = items;
        _loading = false;
      });
    } catch (_) {
      if (!mounted) {
        return;
      }
      setState(() {
        _items = const [];
        _loading = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    ref.listen<bool>(animeListProvider.select((state) => state.isRefreshing), (
      previous,
      next,
    ) {
      if (previous == true && next == false) {
        unawaited(_load());
      }
    });
    if (_loading || _items.isEmpty) {
      return const SizedBox.shrink();
    }

    return Padding(
      key: const Key('continue-bar'),
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            '继续观看',
            style: TextStyle(
              color: AppTheme.foreground,
              fontSize: 16,
              fontWeight: FontWeight.w700,
            ),
          ),
          const SizedBox(height: 10),
          SizedBox(
            height: 104,
            child: ListView.separated(
              scrollDirection: Axis.horizontal,
              itemCount: _items.length,
              separatorBuilder: (context, index) => const SizedBox(width: 10),
              itemBuilder: (context, index) {
                final item = _items[index];
                return _ContinueCard(item: item, onTap: () => unawaited(_open(item)));
              },
            ),
          ),
        ],
      ),
    );
  }

  Future<void> _open(ContinueItem item) async {
    await context.pushNamed(
      'watch',
      pathParameters: {'id': '${item.anime.id}', 'epId': '${item.episode.id}'},
      extra: PlayerLaunchInfo(
        animeTitle: _displayTitle(item.anime),
        episodeNumber: item.episode.epNumber,
      ),
    );
    if (mounted) {
      await _load();
    }
  }
}

class _ContinueCard extends StatelessWidget {
  const _ContinueCard({required this.item, required this.onTap});

  final ContinueItem item;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final title = _displayTitle(item.anime);
    final positionText = item.position > 0
        ? '第${item.episode.epNumber}话 · ${_formatPosition(item.position)}'
        : '第${item.episode.epNumber}话';

    return Semantics(
      button: true,
      label: '$title，$positionText',
      child: Material(
        color: AppTheme.muted,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.all(Radius.circular(12)),
          side: BorderSide(color: AppTheme.border),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          key: Key('continue-item-${item.anime.id}'),
          onTap: onTap,
          child: SizedBox(
            width: 260,
            child: Row(
              children: [
                SizedBox(
                  width: 72,
                  height: 104,
                  child: AnimeCover(
                    animeId: item.anime.id,
                    cover: item.anime.cover,
                    label: '$title封面',
                  ),
                ),
                Expanded(
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(10, 10, 12, 10),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          title,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            color: AppTheme.foreground,
                            fontSize: 14,
                            fontWeight: FontWeight.w600,
                            height: 1.35,
                          ),
                        ),
                        const Spacer(),
                        Text(
                          positionText,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            color: AppTheme.foreground.withValues(alpha: 0.72),
                            fontSize: 12,
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

String _displayTitle(Anime anime) {
  final titleCn = anime.titleCn.trim();
  final title = anime.title.trim();
  if (titleCn.isNotEmpty) {
    return titleCn;
  }
  return title.isEmpty ? '未命名番剧' : title;
}

String _formatPosition(int seconds) {
  final safeSeconds = seconds < 0 ? 0 : seconds;
  final hours = safeSeconds ~/ 3600;
  final minutes = (safeSeconds % 3600) ~/ 60;
  final remainder = safeSeconds % 60;
  if (hours > 0) {
    return '${hours.toString().padLeft(2, '0')}:${minutes.toString().padLeft(2, '0')}:${remainder.toString().padLeft(2, '0')}';
  }
  return '${minutes.toString().padLeft(2, '0')}:${remainder.toString().padLeft(2, '0')}';
}
