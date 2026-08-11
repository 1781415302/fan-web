import 'dart:async';
import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/anime_api.dart';
import '../api/media_api.dart';
import '../api/progress_api.dart';
import '../models/anime.dart';
import '../utils/api_error.dart';
import 'auth_provider.dart';

final animeApiProvider = Provider<AnimeApi>((ref) {
  return AnimeApi(ref.watch(apiClientProvider));
});

final mediaApiProvider = Provider<MediaApi>((ref) {
  return MediaApi(ref.watch(apiClientProvider));
});

final progressApiProvider = Provider<ProgressApi>((ref) {
  return ProgressApi(ref.watch(apiClientProvider));
});

final animeListProvider = NotifierProvider<AnimeListNotifier, AnimeListState>(
  AnimeListNotifier.new,
);

class AnimeListState {
  const AnimeListState({
    this.items = const [],
    this.total = 0,
    this.currentPage = 1,
    this.pageSize = 20,
    this.isLoading = false,
    this.isRefreshing = false,
    this.isLoadingMore = false,
    this.error,
    this.refreshError,
    this.loadMoreError,
  });

  final List<AnimeListItem> items;
  final int total;
  final int currentPage;
  final int pageSize;
  final bool isLoading;
  final bool isRefreshing;
  final bool isLoadingMore;
  final String? error;
  final String? refreshError;
  final String? loadMoreError;

  bool get hasMore => items.length < total;

  AnimeListState copyWith({
    List<AnimeListItem>? items,
    int? total,
    int? currentPage,
    int? pageSize,
    bool? isLoading,
    bool? isRefreshing,
    bool? isLoadingMore,
    String? error,
    String? refreshError,
    String? loadMoreError,
    bool clearError = false,
    bool clearRefreshError = false,
    bool clearLoadMoreError = false,
  }) {
    return AnimeListState(
      items: items ?? this.items,
      total: total ?? this.total,
      currentPage: currentPage ?? this.currentPage,
      pageSize: pageSize ?? this.pageSize,
      isLoading: isLoading ?? this.isLoading,
      isRefreshing: isRefreshing ?? this.isRefreshing,
      isLoadingMore: isLoadingMore ?? this.isLoadingMore,
      error: error ?? (clearError ? null : this.error),
      refreshError: refreshError ??
          (clearRefreshError ? null : this.refreshError),
      loadMoreError: loadMoreError ??
          (clearLoadMoreError ? null : this.loadMoreError),
    );
  }
}

class AnimeListNotifier extends Notifier<AnimeListState> {
  static const _cacheKeyPrefix = 'anime_list_cache_v2';

  late AnimeApi _animeApi;

  /// 构建按服务器和用户隔离的缓存键（使用 base64 避免碰撞）
  String _cacheKey() {
    final auth = ref.read(authProvider);
    final serverUrl = auth.serverUrl ?? '';
    final userId = auth.user?.id ?? 0;
    final encoded = base64Url.encode(utf8.encode(serverUrl));
    return '${_cacheKeyPrefix}_$encoded _$userId';
  }

  /// 清除当前用户的缓存（登出时调用）
  Future<void> clearCache() async {
    try {
      final prefs = ref.read(sharedPreferencesProvider);
      await prefs.remove(_cacheKey());
    } catch (_) {}
  }

  @override
  AnimeListState build() {
    // 监听用户身份和服务器变化（不只看 token 字符串）
    ref.watch(authProvider.select((s) => (s.serverUrl, s.user?.id)));
    _animeApi = ref.watch(animeApiProvider);
    return const AnimeListState();
  }

  Future<void> loadFirstPage() async {
    if (state.isLoading || state.isRefreshing || state.isLoadingMore) {
      return;
    }

    final cached = _loadFromCache();
    if (cached != null && cached.items.isNotEmpty) {
      state = state.copyWith(
        items: cached.items,
        total: cached.total,
        currentPage: cached.page,
        pageSize: cached.pageSize == 0 ? state.pageSize : cached.pageSize,
        isLoading: false,
        isRefreshing: true,
        clearError: true,
        clearRefreshError: true,
      );
      unawaited(_fetchFirstPage());
      return;
    }

    state = state.copyWith(
      isLoading: true,
      isRefreshing: false,
      isLoadingMore: false,
      clearError: true,
      clearRefreshError: true,
      clearLoadMoreError: true,
    );
    await _fetchFirstPage();
  }

  Future<void> _fetchFirstPage() async {
    try {
      final result = await _animeApi.list(page: 1, pageSize: state.pageSize);
      state = state.copyWith(
        items: result.items,
        total: result.total,
        currentPage: result.page,
        pageSize: result.pageSize == 0 ? state.pageSize : result.pageSize,
        isLoading: false,
        isRefreshing: false,
        clearError: true,
        clearRefreshError: true,
      );
      _saveToCache(result);
    } catch (error) {
      final msg = describeApiError(error);
      if (state.items.isEmpty) {
        state = state.copyWith(
          isLoading: false,
          isRefreshing: false,
          error: msg,
        );
      } else {
        state = state.copyWith(isRefreshing: false, refreshError: msg);
      }
    }
  }

  void _saveToCache(PaginatedAnimes result) {
    try {
      final prefs = ref.read(sharedPreferencesProvider);
      final cacheData = jsonEncode({
        'items': result.items.map((item) => item.toJson()).toList(),
        'total': result.total,
        'page': result.page,
        'page_size': result.pageSize,
      });
      prefs.setString(_cacheKey(), cacheData);
    } catch (_) {}
  }

  PaginatedAnimes? _loadFromCache() {
    try {
      final prefs = ref.read(sharedPreferencesProvider);
      final cached = prefs.getString(_cacheKey());
      if (cached == null || cached.isEmpty) return null;
      final json = jsonDecode(cached) as Map<String, dynamic>;
      return PaginatedAnimes.fromJson(json);
    } catch (_) {
      return null;
    }
  }

  Future<void> refresh() async {
    if (state.isLoading || state.isRefreshing || state.isLoadingMore) {
      return;
    }

    state = state.copyWith(
      isRefreshing: true,
      clearRefreshError: true,
    );
    try {
      final result = await _animeApi.list(page: 1, pageSize: state.pageSize);
      state = state.copyWith(
        items: result.items,
        total: result.total,
        currentPage: result.page,
        pageSize: result.pageSize == 0 ? state.pageSize : result.pageSize,
        isRefreshing: false,
        clearError: true,
        clearRefreshError: true,
      );
      _saveToCache(result);
    } catch (error) {
      state = state.copyWith(
        isRefreshing: false,
        refreshError: describeApiError(error),
      );
    }
  }

  Future<void> loadNextPage() async {
    if (state.isLoading ||
        state.isRefreshing ||
        state.isLoadingMore ||
        !state.hasMore) {
      return;
    }

    final nextPage = state.currentPage + 1;
    state = state.copyWith(isLoadingMore: true, clearLoadMoreError: true);
    try {
      final result = await _animeApi.list(
        page: nextPage,
        pageSize: state.pageSize,
      );
      state = state.copyWith(
        items: [...state.items, ...result.items],
        total: result.total,
        currentPage: result.page,
        pageSize: result.pageSize == 0 ? state.pageSize : result.pageSize,
        isLoadingMore: false,
        clearLoadMoreError: true,
      );
    } catch (error) {
      state = state.copyWith(
        isLoadingMore: false,
        loadMoreError: describeApiError(error),
      );
    }
  }
}

String apiErrorMessage(Object error) {
  return describeApiError(error);
}
