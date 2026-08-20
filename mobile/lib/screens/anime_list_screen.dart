import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../api/anime_api.dart';
import '../api/library_api.dart';
import '../models/anime.dart';
import '../providers/anime_provider.dart';
import '../providers/auth_provider.dart';
import '../theme/app_theme.dart';
import '../widgets/anime_card.dart';
import '../widgets/continue_bar.dart';
import '../widgets/state_widgets.dart';
import '../widgets/user_sheet.dart';

class AnimeListScreen extends ConsumerStatefulWidget {
  const AnimeListScreen({super.key});

  @override
  ConsumerState<AnimeListScreen> createState() => _AnimeListScreenState();
}

class _AnimeListScreenState extends ConsumerState<AnimeListScreen> {
  late final ScrollController _scrollController;
  late final TextEditingController _searchController;
  Timer? _searchDebounce;
  String _keyword = '';

  List<AnimeListItem> _searchItems = const [];
  int _searchTotal = 0;
  int _searchPage = 1;
  bool _searchLoading = false;
  bool _searchLoadingMore = false;
  String? _searchError;
  String? _searchLoadMoreError;

  bool _scanPolling = false;
  ScanJob? _scanJob;
  String? _scanError;
  String? _scanHint;
  List<UnidentifiedFile> _unidentified = const [];
  String? _unidentifiedError;
  String? _confirmingPath;
  bool _loadingUnidentified = false;

  bool get _isSearching => _keyword.trim().isNotEmpty;

  @override
  void initState() {
    super.initState();
    _scrollController = ScrollController()..addListener(_onScroll);
    _searchController = TextEditingController();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) {
        return;
      }
      final state = ref.read(animeListProvider);
      if (state.items.isEmpty && !state.isLoading && state.error == null) {
        unawaited(ref.read(animeListProvider.notifier).loadFirstPage());
      }
      if (ref.read(authProvider).user?.isAdmin == true) {
        unawaited(_loadUnidentified());
      }
    });
  }

  @override
  void dispose() {
    _searchDebounce?.cancel();
    _searchController.dispose();
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

    if (_isSearching) {
      if (_searchItems.length < _searchTotal &&
          !_searchLoading &&
          !_searchLoadingMore &&
          _searchError == null) {
        unawaited(_loadSearchNext());
      }
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

  void _onSearchChanged(String value) {
    _searchDebounce?.cancel();
    _searchDebounce = Timer(const Duration(milliseconds: 300), () {
      if (!mounted) {
        return;
      }
      final keyword = value.trim();
      setState(() {
        _keyword = keyword;
        _searchError = null;
        _searchLoadMoreError = null;
      });
      if (keyword.isEmpty) {
        setState(() {
          _searchItems = const [];
          _searchTotal = 0;
          _searchPage = 1;
        });
        return;
      }
      unawaited(_runSearch(keyword));
    });
  }

  Future<void> _runSearch(String keyword) async {
    setState(() {
      _searchLoading = true;
      _searchError = null;
      _searchLoadMoreError = null;
      _searchPage = 1;
    });
    try {
      final result = await ref
          .read(animeApiProvider)
          .list(page: 1, pageSize: 20, keyword: keyword);
      if (!mounted || _keyword.trim() != keyword) {
        return;
      }
      setState(() {
        _searchItems = result.items;
        _searchTotal = result.total;
        _searchPage = result.page == 0 ? 1 : result.page;
        _searchLoading = false;
      });
    } catch (error) {
      if (!mounted || _keyword.trim() != keyword) {
        return;
      }
      setState(() {
        _searchLoading = false;
        _searchError = apiErrorMessage(error);
      });
    }
  }

  Future<void> _loadSearchNext() async {
    final keyword = _keyword.trim();
    if (keyword.isEmpty || _searchLoadingMore) {
      return;
    }
    final nextPage = _searchPage + 1;
    setState(() {
      _searchLoadingMore = true;
      _searchLoadMoreError = null;
    });
    try {
      final result = await ref
          .read(animeApiProvider)
          .list(page: nextPage, pageSize: 20, keyword: keyword);
      if (!mounted || _keyword.trim() != keyword) {
        return;
      }
      setState(() {
        _searchItems = [..._searchItems, ...result.items];
        _searchTotal = result.total;
        _searchPage = result.page == 0 ? nextPage : result.page;
        _searchLoadingMore = false;
      });
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _searchLoadingMore = false;
        _searchLoadMoreError = apiErrorMessage(error);
      });
    }
  }

  Future<void> _refresh() async {
    if (_isSearching) {
      await _runSearch(_keyword.trim());
    } else {
      await ref.read(animeListProvider.notifier).refresh();
    }
    if (ref.read(authProvider).user?.isAdmin == true) {
      await _loadUnidentified();
    }
  }

  Future<void> _loadUnidentified() async {
    setState(() {
      _loadingUnidentified = true;
      _unidentifiedError = null;
    });
    try {
      final items = <UnidentifiedFile>[];
      var page = 1;
      var total = 0;
      while (true) {
        final result = await ref
            .read(libraryApiProvider)
            .listUnidentified(page: page, pageSize: 100);
        total = result.total;
        items.addAll(result.items);
        if (items.length >= total || result.items.isEmpty) {
          break;
        }
        page++;
      }
      if (!mounted) {
        return;
      }
      setState(() {
        _unidentified = items;
        _loadingUnidentified = false;
      });
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _loadingUnidentified = false;
        _unidentifiedError = apiErrorMessage(error);
      });
    }
  }

  Future<void> _startScan() async {
    if (_scanPolling) {
      return;
    }
    setState(() {
      _scanPolling = true;
      _scanError = null;
      _scanHint = null;
      _scanJob = null;
    });
    try {
      final job = await pollLibraryScan(
        api: ref.read(libraryApiProvider),
        shouldStop: () => !mounted,
        onUpdate: (current) {
          if (mounted) {
            setState(() => _scanJob = current);
          }
        },
      );
      if (!mounted) {
        return;
      }
      setState(() {
        _scanJob = job;
        _scanPolling = false;
        if (job.state == 'error') {
          _scanError = (job.error != null && job.error!.isNotEmpty)
              ? job.error
              : '库扫描失败';
        } else if (job.isRunning) {
          _scanHint = '扫描仍在服务器执行，已停止轮询';
        }
      });
      if (job.state == 'done') {
        await _loadUnidentified();
        await ref.read(animeListProvider.notifier).refresh();
      }
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _scanPolling = false;
        _scanError = apiErrorMessage(error);
      });
    }
  }

  Future<void> _confirmCandidate(
    UnidentifiedFile file,
    MatchCandidate candidate,
  ) async {
    if (_confirmingPath != null) {
      return;
    }
    setState(() {
      _confirmingPath = file.filePath;
      _scanError = null;
    });
    try {
      await confirmUnidentified(
        animeApi: ref.read(animeApiProvider),
        bangumiId: candidate.id,
        filePath: file.filePath,
      );
      if (!mounted) {
        return;
      }
      setState(() {
        _unidentified = _unidentified
            .where((item) => item.filePath != file.filePath)
            .toList(growable: false);
      });
      await ref.read(animeListProvider.notifier).refresh();
      await _loadUnidentified();
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _scanError = apiErrorMessage(error);
      });
    } finally {
      if (mounted && _confirmingPath == file.filePath) {
        setState(() {
          _confirmingPath = null;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(animeListProvider);
    final isAdmin = ref.watch(
      authProvider.select((auth) => auth.user?.isAdmin == true),
    );
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
          slivers: [
            const SliverToBoxAdapter(child: ContinueBar()),
            ..._buildToolbarSlivers(isAdmin),
            ..._buildContentSlivers(state),
          ],
        ),
      ),
    );
  }

  List<Widget> _buildToolbarSlivers(bool isAdmin) {
    return [
      SliverToBoxAdapter(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
          child: TextField(
            key: const Key('anime-search'),
            controller: _searchController,
            onChanged: _onSearchChanged,
            textInputAction: TextInputAction.search,
            decoration: const InputDecoration(
              hintText: '搜索番剧',
              prefixIcon: Icon(Icons.search),
            ),
          ),
        ),
      ),
      if (isAdmin)
        SliverToBoxAdapter(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: Wrap(
              spacing: 10,
              runSpacing: 10,
              children: [
                OutlinedButton.icon(
                  key: const Key('library-scan'),
                  onPressed: _scanPolling
                      ? null
                      : () => unawaited(_startScan()),
                  icon: const Icon(Icons.sync),
                  label: Text(_scanPolling ? '扫描中...' : '库扫描'),
                ),
                FilledButton.icon(
                  key: const Key('anime-add'),
                  onPressed: () => context.pushNamed('animeAdd'),
                  icon: const Icon(Icons.add),
                  label: const Text('添加番剧'),
                ),
              ],
            ),
          ),
        ),
      if (isAdmin) ..._buildScanSlivers(),
    ];
  }

  List<Widget> _buildScanSlivers() {
    final slivers = <Widget>[];
    if (_scanPolling && (_scanJob == null || _scanJob!.isRunning)) {
      slivers.add(
        const SliverToBoxAdapter(
          child: Padding(
            padding: EdgeInsets.fromLTRB(16, 4, 16, 8),
            child: Row(
              children: [
                SizedBox.square(
                  dimension: 18,
                  child: CircularProgressIndicator(strokeWidth: 2),
                ),
                SizedBox(width: 10),
                Text('扫描中...'),
              ],
            ),
          ),
        ),
      );
    }
    final result = _scanJob?.result;
    if (_scanJob?.state == 'done' && result != null) {
      slivers.add(
        SliverToBoxAdapter(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
            child: _ScanResultCard(result: result),
          ),
        ),
      );
    }
    if (_scanHint != null) {
      slivers.add(
        SliverToBoxAdapter(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: Text(
              _scanHint!,
              style: const TextStyle(color: AppTheme.warning, fontSize: 13),
            ),
          ),
        ),
      );
    }
    if (_scanError != null) {
      slivers.add(
        SliverToBoxAdapter(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: Text(
              _scanError!,
              style: const TextStyle(color: AppTheme.destructive, fontSize: 13),
            ),
          ),
        ),
      );
    }
    if (_unidentifiedError != null) {
      slivers.add(
        SliverToBoxAdapter(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: Text(
              _unidentifiedError!,
              style: const TextStyle(color: AppTheme.destructive, fontSize: 13),
            ),
          ),
        ),
      );
    }
    if (_loadingUnidentified && _unidentified.isEmpty) {
      slivers.add(
        const SliverToBoxAdapter(
          child: Padding(
            padding: EdgeInsets.fromLTRB(16, 4, 16, 8),
            child: Center(
              child: SizedBox.square(
                dimension: 20,
                child: CircularProgressIndicator(strokeWidth: 2),
              ),
            ),
          ),
        ),
      );
    }
    if (_unidentified.isNotEmpty) {
      slivers.add(
        SliverToBoxAdapter(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
            child: _UnidentifiedPanel(
              files: _unidentified,
              confirmingPath: _confirmingPath,
              onConfirm: _confirmCandidate,
            ),
          ),
        ),
      );
    }
    return slivers;
  }

  List<Widget> _buildContentSlivers(AnimeListState state) {
    if (_isSearching) {
      return _buildSearchSlivers();
    }
    return _buildLibrarySlivers(state);
  }

  List<Widget> _buildSearchSlivers() {
    if (_searchItems.isEmpty && _searchLoading) {
      return [_buildGridSliver(const [], skeleton: true)];
    }
    if (_searchItems.isEmpty && _searchError != null) {
      return [
        SliverFillRemaining(
          hasScrollBody: false,
          child: ErrorStateView(
            message: _searchError!,
            onRetry: () => unawaited(_runSearch(_keyword.trim())),
          ),
        ),
      ];
    }
    if (_searchItems.isEmpty) {
      return [
        const SliverFillRemaining(
          hasScrollBody: false,
          child: EmptyStateView(icon: Icons.search_off, message: '没有找到相关番剧'),
        ),
      ];
    }
    final slivers = <Widget>[_buildGridSliver(_searchItems)];
    if (_searchLoadingMore) {
      slivers.add(_loadingMoreSliver());
    } else if (_searchLoadMoreError != null) {
      slivers.add(
        SliverToBoxAdapter(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 4, 16, 24),
            child: Center(
              child: OutlinedButton.icon(
                onPressed: () => unawaited(_loadSearchNext()),
                icon: const Icon(Icons.refresh),
                label: Text(_searchLoadMoreError!),
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

  List<Widget> _buildLibrarySlivers(AnimeListState state) {
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
        const SliverFillRemaining(
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
      slivers.add(_loadingMoreSliver());
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

  Widget _loadingMoreSliver() {
    return const SliverToBoxAdapter(
      child: Padding(
        padding: EdgeInsets.only(bottom: 24),
        child: Center(
          child: SizedBox.square(
            dimension: 24,
            child: CircularProgressIndicator(strokeWidth: 2),
          ),
        ),
      ),
    );
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

class _ScanResultCard extends StatelessWidget {
  const _ScanResultCard({required this.result});

  final LibraryScanResult result;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: AppTheme.muted,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: AppTheme.border),
      ),
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              '库扫描完成',
              style: TextStyle(
                color: AppTheme.foreground,
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(height: 10),
            Text(
              '视频 ${result.totalFiles} · 新增番剧 ${result.newAnimes} · 新增集数 ${result.newEpisodes} · 无法识别 ${result.unidentified.length}',
              style: TextStyle(
                color: AppTheme.foreground.withValues(alpha: 0.72),
                fontSize: 13,
              ),
            ),
            const SizedBox(height: 6),
            Text(
              '跳过 ${result.skipped} 个已关联文件',
              style: TextStyle(
                color: AppTheme.foreground.withValues(alpha: 0.58),
                fontSize: 12,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _UnidentifiedPanel extends StatelessWidget {
  const _UnidentifiedPanel({
    required this.files,
    required this.confirmingPath,
    required this.onConfirm,
  });

  final List<UnidentifiedFile> files;
  final String? confirmingPath;
  final void Function(UnidentifiedFile file, MatchCandidate candidate)
  onConfirm;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: AppTheme.muted,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: AppTheme.border),
      ),
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '未识别 ${files.length}',
              style: const TextStyle(
                color: AppTheme.warning,
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(height: 10),
            for (final file in files) ...[
              Text(
                file.fileName,
                style: const TextStyle(
                  color: AppTheme.foreground,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 2),
              Text(
                file.reason,
                style: TextStyle(
                  color: AppTheme.foreground.withValues(alpha: 0.62),
                  fontSize: 12,
                ),
              ),
              if (file.candidates.isNotEmpty) ...[
                const SizedBox(height: 8),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final candidate in file.candidates)
                      OutlinedButton(
                        key: Key('candidate-${file.filePath}-${candidate.id}'),
                        onPressed: confirmingPath == file.filePath
                            ? null
                            : () => onConfirm(file, candidate),
                        child: Text(
                          '${candidate.displayName} ${candidate.score.toStringAsFixed(2)}',
                        ),
                      ),
                  ],
                ),
              ],
              const SizedBox(height: 12),
            ],
          ],
        ),
      ),
    );
  }
}
