import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../models/anime.dart';
import '../providers/anime_provider.dart';
import '../theme/app_theme.dart';
import '../widgets/anime_card.dart';
import '../widgets/state_widgets.dart';
import '../widgets/user_sheet.dart';

class AnimeListScreen extends ConsumerStatefulWidget {
  const AnimeListScreen({super.key});

  @override
  ConsumerState<AnimeListScreen> createState() => _AnimeListScreenState();
}

class _AnimeListScreenState extends ConsumerState<AnimeListScreen> {
  late final ScrollController _scrollController;

  @override
  void initState() {
    super.initState();
    _scrollController = ScrollController()..addListener(_onScroll);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) {
        return;
      }
      final state = ref.read(animeListProvider);
      if (state.items.isEmpty && !state.isLoading && state.error == null) {
        unawaited(ref.read(animeListProvider.notifier).loadFirstPage());
      }
    });
  }

  @override
  void dispose() {
    _scrollController
      ..removeListener(_onScroll)
      ..dispose();
    super.dispose();
  }

  void _onScroll() {
    if (!_scrollController.hasClients) {
      return;
    }
    final distanceToBottom =
        _scrollController.position.maxScrollExtent -
        _scrollController.position.pixels;
    if (distanceToBottom > 240) {
      return;
    }

    final state = ref.read(animeListProvider);
    if (state.hasMore &&
        !state.isLoading &&
        !state.isRefreshing &&
        !state.isLoadingMore &&
        state.error == null) {
      unawaited(ref.read(animeListProvider.notifier).loadNextPage());
    }
  }

  Future<void> _refresh() {
    return ref.read(animeListProvider.notifier).refresh();
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(animeListProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('番剧库'),
        actions: [
          IconButton(
            tooltip: '账号',
            onPressed: () => showUserSheet(context, ref),
            icon: const Icon(Icons.account_circle_outlined),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: _refresh,
        color: AppTheme.accent,
        backgroundColor: AppTheme.muted,
        child: CustomScrollView(
          controller: _scrollController,
          physics: const AlwaysScrollableScrollPhysics(),
          slivers: _buildSlivers(state),
        ),
      ),
    );
  }

  List<Widget> _buildSlivers(AnimeListState state) {
    if (state.items.isEmpty && state.isLoading) {
      return [_buildGridSliver(const [], skeleton: true)];
    }
    if (state.items.isEmpty && state.error != null) {
      return [
        SliverFillRemaining(
          hasScrollBody: false,
          child: ErrorStateView(
            message: state.error!,
            onRetry: () {
              unawaited(ref.read(animeListProvider.notifier).loadFirstPage());
            },
          ),
        ),
      ];
    }
    if (state.items.isEmpty) {
      return [
        SliverFillRemaining(
          hasScrollBody: false,
          child: EmptyStateView(
            icon: Icons.video_library_outlined,
            message: '暂无番剧',
          ),
        ),
      ];
    }

    final slivers = <Widget>[_buildGridSliver(state.items)];
    if (state.refreshError != null) {
      slivers.add(
        SliverToBoxAdapter(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
            child: Center(
              child: Text(
                '刷新失败：${state.refreshError}（显示缓存内容）',
                textAlign: TextAlign.center,
                style: TextStyle(
                  color: AppTheme.foreground.withValues(alpha: 0.6),
                  fontSize: 13,
                ),
              ),
            ),
          ),
        ),
      );
    }
    if (state.isLoadingMore) {
      slivers.add(
        const SliverToBoxAdapter(
          child: Padding(
            padding: EdgeInsets.only(bottom: 24),
            child: Center(
              child: SizedBox.square(
                dimension: 24,
                child: CircularProgressIndicator(strokeWidth: 2),
              ),
            ),
          ),
        ),
      );
    } else if (state.loadMoreError != null) {
      slivers.add(
        SliverToBoxAdapter(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 4, 16, 24),
            child: Center(
              child: OutlinedButton.icon(
                onPressed: () {
                  unawaited(
                    ref.read(animeListProvider.notifier).loadNextPage(),
                  );
                },
                icon: const Icon(Icons.refresh),
                label: Text(state.loadMoreError!),
              ),
            ),
          ),
        ),
      );
    } else {
      slivers.add(const SliverToBoxAdapter(child: SizedBox(height: 24)));
    }
    return slivers;
  }

  Widget _buildGridSliver(List<AnimeListItem> items, {bool skeleton = false}) {
    final itemCount = skeleton ? 6 : items.length;
    return SliverPadding(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
      sliver: SliverLayoutBuilder(
        builder: (context, constraints) {
          final columnCount = constraints.crossAxisExtent >= 900
              ? 6
              : constraints.crossAxisExtent >= 600
              ? 5
              : 3;
          final gap = (columnCount - 1) * 10;
          final tileWidth = (constraints.crossAxisExtent - gap) / columnCount;
          final tileHeight = tileWidth * 1.5 + 90;
          return SliverGrid(
            delegate: SliverChildBuilderDelegate((context, index) {
              if (skeleton) {
                return const AnimeCardSkeleton();
              }
              final item = items[index];
              return AnimeCard(
                item: item,
                onTap: () {
                  context.pushNamed(
                    'animeDetail',
                    pathParameters: {'id': '${item.anime.id}'},
                  );
                },
              );
            }, childCount: itemCount),
            gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: columnCount,
              crossAxisSpacing: 10,
              mainAxisSpacing: 14,
              mainAxisExtent: tileHeight,
            ),
          );
        },
      ),
    );
  }
}
