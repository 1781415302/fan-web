import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/anime.dart';
import '../providers/auth_provider.dart';
import '../theme/app_theme.dart';

class AnimeCard extends StatelessWidget {
  const AnimeCard({required this.item, required this.onTap, super.key});

  final AnimeListItem item;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final anime = item.anime;
    final title = _displayTitle(anime);
    final watchedCount = item.watchedCount.clamp(0, anime.epCount);
    final progress = anime.epCount <= 0
        ? 0.0
        : (watchedCount / anime.epCount).clamp(0.0, 1.0).toDouble();
    final progressText = anime.epCount > 0
        ? '已看 $watchedCount / 全${anime.epCount}话'
        : '已看 $watchedCount / 集数未知';

    return Semantics(
      button: true,
      label: '$title，$progressText',
      child: Material(
        color: AppTheme.muted,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.all(Radius.circular(12)),
          side: BorderSide(color: AppTheme.border),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onTap,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              AspectRatio(
                aspectRatio: 2 / 3,
                child: AnimeCover(
                  animeId: anime.id,
                  cover: anime.cover,
                  label: '$title封面',
                ),
              ),
              Padding(
                padding: const EdgeInsets.fromLTRB(8, 8, 8, 10),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    SizedBox(
                      height: 40,
                      child: Text(
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
                    ),
                    const SizedBox(height: 4),
                    Text(
                      progressText,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        color: AppTheme.foreground.withValues(alpha: 0.72),
                        fontSize: 12,
                        height: 1.3,
                      ),
                    ),
                    const SizedBox(height: 6),
                    Semantics(
                      label: '观看进度 ${(progress * 100).round()}%',
                      child: ClipRRect(
                        borderRadius: BorderRadius.circular(999),
                        child: LinearProgressIndicator(
                          value: progress,
                          minHeight: 4,
                          backgroundColor: AppTheme.border,
                          valueColor: const AlwaysStoppedAnimation<Color>(
                            AppTheme.accent,
                          ),
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  static String _displayTitle(Anime anime) {
    final chineseTitle = anime.titleCn.trim();
    final originalTitle = anime.title.trim();
    if (chineseTitle.isNotEmpty) {
      return chineseTitle;
    }
    return originalTitle.isEmpty ? '未命名番剧' : originalTitle;
  }
}

class AnimeCardSkeleton extends StatelessWidget {
  const AnimeCardSkeleton({super.key});

  @override
  Widget build(BuildContext context) {
    return Material(
      color: AppTheme.muted,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.all(Radius.circular(12)),
        side: BorderSide(color: AppTheme.border),
      ),
      clipBehavior: Clip.antiAlias,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const AspectRatio(
            aspectRatio: 2 / 3,
            child: ColoredBox(color: AppTheme.coverPlaceholder),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(8, 8, 8, 10),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Container(
                  height: 16,
                  decoration: BoxDecoration(
                    color: AppTheme.foreground.withValues(alpha: 0.16),
                    borderRadius: BorderRadius.circular(4),
                  ),
                ),
                const SizedBox(height: 8),
                Container(
                  height: 14,
                  width: 64,
                  decoration: BoxDecoration(
                    color: AppTheme.foreground.withValues(alpha: 0.1),
                    borderRadius: BorderRadius.circular(4),
                  ),
                ),
                const SizedBox(height: 8),
                const LinearProgressIndicator(
                  value: 0,
                  minHeight: 4,
                  backgroundColor: AppTheme.border,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class AnimeCover extends ConsumerWidget {
  const AnimeCover({
    required this.animeId,
    required this.cover,
    required this.label,
    this.borderRadius = BorderRadius.zero,
    super.key,
  });

  final int animeId;
  final String cover;
  final String label;
  final BorderRadius borderRadius;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final authState = ref.watch(authProvider);
    final proxyUrl = _coverProxyUrl(authState);
    final imageUrl = proxyUrl ?? cover.trim();
    final headers = authState.token == null || authState.token!.isEmpty
        ? null
        : <String, String>{'Authorization': 'Bearer ${authState.token}'};
    final placeholder = _placeholder();
    final image = imageUrl.isEmpty
        ? placeholder
        : CachedNetworkImage(
            imageUrl: imageUrl,
            httpHeaders: headers,
            fit: BoxFit.cover,
            memCacheWidth: 300,
            placeholder: (context, url) => Stack(
              fit: StackFit.expand,
              children: [
                placeholder,
                Center(
                  child: SizedBox.square(
                    dimension: 24,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: AppTheme.accent,
                    ),
                  ),
                ),
              ],
            ),
            errorWidget: (context, url, error) => placeholder,
          );

    return Semantics(
      image: true,
      label: label,
      child: ClipRRect(
        borderRadius: borderRadius,
        child: ColoredBox(color: AppTheme.muted, child: image),
      ),
    );
  }

  String? _coverProxyUrl(AuthState authState) {
    final serverUrl = authState.serverUrl?.trim();
    if (animeId <= 0 || serverUrl == null || serverUrl.isEmpty) {
      return null;
    }
    final normalized = serverUrl.replaceFirst(RegExp(r'/+$'), '');
    return '$normalized/api/animes/$animeId/cover';
  }

  Widget _placeholder() {
    return ColoredBox(
      color: AppTheme.coverPlaceholder,
      child: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.image_not_supported_outlined,
              color: AppTheme.foreground.withValues(alpha: 0.5),
              size: 26,
            ),
            const SizedBox(height: 6),
            Text(
              '无封面',
              style: TextStyle(
                color: AppTheme.foreground.withValues(alpha: 0.62),
                fontSize: 12,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
