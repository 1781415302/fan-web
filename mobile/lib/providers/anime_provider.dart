import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/anime_api.dart';
import '../api/progress_api.dart';
import '../models/anime.dart';
import '../utils/api_error.dart';
import 'auth_provider.dart';

final animeApiProvider = Provider<AnimeApi>((ref) {
  return AnimeApi(ref.watch(apiClientProvider));
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
  });

  final List<AnimeListItem> items;
  final int total;
  final int currentPage;
  final int pageSize;
  final bool isLoading;
  final bool isRefreshing;
  final bool isLoadingMore;
  final String? error;

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
    bool clearError = false,
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
    );
  }
}

class AnimeListNotifier extends Notifier<AnimeListState> {
  late AnimeApi _animeApi;

  @override
  AnimeListState build() {
    // Reset list state whenever the authenticated account changes.
    ref.watch(authProvider.select((authState) => authState.token));
    _animeApi = ref.watch(animeApiProvider);
    return const AnimeListState();
  }

  Future<void> loadFirstPage() async {
    if (state.isLoading || state.isRefreshing || state.isLoadingMore) {
      return;
    }

    state = state.copyWith(
      isLoading: true,
      isRefreshing: false,
      isLoadingMore: false,
      clearError: true,
    );
    try {
      final result = await _animeApi.list(page: 1, pageSize: state.pageSize);
      state = state.copyWith(
        items: result.items,
        total: result.total,
        currentPage: result.page,
        pageSize: result.pageSize == 0 ? state.pageSize : result.pageSize,
        isLoading: false,
        clearError: true,
      );
    } catch (error) {
      state = state.copyWith(isLoading: false, error: apiErrorMessage(error));
    }
  }

  Future<void> refresh() async {
    if (state.isLoading || state.isRefreshing || state.isLoadingMore) {
      return;
    }

    state = state.copyWith(isRefreshing: true, clearError: true);
    try {
      final result = await _animeApi.list(page: 1, pageSize: state.pageSize);
      state = state.copyWith(
        items: result.items,
        total: result.total,
        currentPage: result.page,
        pageSize: result.pageSize == 0 ? state.pageSize : result.pageSize,
        isRefreshing: false,
        clearError: true,
      );
    } catch (error) {
      state = state.copyWith(
        isRefreshing: false,
        error: apiErrorMessage(error),
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
    state = state.copyWith(isLoadingMore: true, clearError: true);
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
        clearError: true,
      );
    } catch (error) {
      state = state.copyWith(
        isLoadingMore: false,
        error: apiErrorMessage(error),
      );
    }
  }
}

String apiErrorMessage(Object error) {
  return describeApiError(error);
}
