import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:fan_web/api/api_client.dart';
import 'package:fan_web/providers/auth_provider.dart';
import 'package:fan_web/router.dart';

import 'library_test_fakes.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<GoRouter> routerFor(AuthState auth) async {
    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    final container = ProviderContainer(
      overrides: [
        sharedPreferencesProvider.overrideWithValue(prefs),
        apiClientProvider.overrideWithValue(ApiClient()),
        authProvider.overrideWith(() => FixedAuthNotifier(auth)),
      ],
    );
    addTearDown(container.dispose);
    return container.read(routerProvider);
  }

  test(
    'registers /animes/new before /animes/:id and keeps one watch route',
    () async {
      final router = await routerFor(adminAuthState());
      final paths = router.configuration.routes
          .whereType<GoRoute>()
          .map((route) => route.path)
          .toList();
      expect(paths.indexOf('/animes/new'), greaterThanOrEqualTo(0));
      expect(
        paths.indexOf('/animes/:id'),
        greaterThan(paths.indexOf('/animes/new')),
      );
      expect(paths.where((path) => path.contains('watch')).length, 1);
    },
  );
}
