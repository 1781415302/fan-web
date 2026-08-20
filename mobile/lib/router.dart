import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'providers/auth_provider.dart';
import 'providers/player_provider.dart';
import 'screens/anime_add_screen.dart';
import 'screens/anime_detail_screen.dart';
import 'screens/anime_list_screen.dart';
import 'screens/player_screen.dart';
import 'screens/server_login_screen.dart';
import 'theme/app_theme.dart';

final routerProvider = Provider<GoRouter>((ref) {
  final router = GoRouter(
    initialLocation: '/',
    redirect: (context, state) {
      final authState = ref.read(authProvider);
      final location = state.matchedLocation;

      if (authState.status == AuthStatus.initial) {
        return location == '/' ? null : '/';
      }
      if (authState.status == AuthStatus.unauthenticated) {
        return location == '/login' ? null : '/login';
      }
      if (location == '/login') {
        return '/';
      }
      if (location == '/animes/new' && authState.user?.isAdmin != true) {
        return '/';
      }
      return null;
    },
    routes: [
      GoRoute(
        path: '/login',
        name: 'login',
        builder: (context, state) => const ServerLoginScreen(),
      ),
      GoRoute(
        path: '/',
        name: 'home',
        builder: (context, state) => const _HomeRouteScreen(),
      ),
      GoRoute(
        path: '/animes/new',
        name: 'animeAdd',
        builder: (context, state) => const AnimeAddScreen(),
      ),
      GoRoute(
        path: '/animes/:id',
        name: 'animeDetail',
        builder: (context, state) {
          final animeId = int.tryParse(state.pathParameters['id'] ?? '');
          if (animeId == null || animeId <= 0) {
            return const _InvalidRouteScreen();
          }
          return AnimeDetailScreen(animeId: animeId);
        },
      ),
      GoRoute(
        path: '/animes/:id/watch/:epId',
        name: 'watch',
        builder: (context, state) {
          final animeId = int.tryParse(state.pathParameters['id'] ?? '');
          final episodeId = int.tryParse(state.pathParameters['epId'] ?? '');
          if (animeId == null ||
              animeId <= 0 ||
              episodeId == null ||
              episodeId <= 0) {
            return const _InvalidRouteScreen();
          }
          final launchInfo = state.extra;
          final launch = launchInfo is PlayerLaunchInfo ? launchInfo : null;
          return PlayerScreen(
            animeId: animeId,
            episodeId: episodeId,
            animeTitle: launch?.animeTitle,
            episodeNumber: launch?.episodeNumber,
          );
        },
      ),
    ],
  );

  ref.listen<AuthState>(authProvider, (previous, next) {
    if (previous?.status != next.status) {
      router.refresh();
    }
  });
  ref.onDispose(router.dispose);
  return router;
});

class _HomeRouteScreen extends ConsumerWidget {
  const _HomeRouteScreen();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final status = ref.watch(authProvider.select((state) => state.status));
    return switch (status) {
      AuthStatus.initial => const _StartupScreen(),
      AuthStatus.authenticated => const AnimeListScreen(),
      AuthStatus.unauthenticated => const _StartupScreen(),
    };
  }
}

class _StartupScreen extends StatelessWidget {
  const _StartupScreen();

  @override
  Widget build(BuildContext context) {
    return const Scaffold(
      body: SafeArea(
        child: Center(child: CircularProgressIndicator(color: AppTheme.accent)),
      ),
    );
  }
}

class _InvalidRouteScreen extends StatelessWidget {
  const _InvalidRouteScreen();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(),
      body: Center(
        child: Text('番剧地址无效', style: Theme.of(context).textTheme.titleMedium),
      ),
    );
  }
}
