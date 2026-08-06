import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../api/api_client.dart';
import '../api/auth_api.dart';
import '../models/user.dart';

const fanWebTokenKey = 'fan_web_token';
const fanWebServerUrlKey = 'fan_web_server_url';

enum AuthStatus { initial, authenticated, unauthenticated }

class AuthState {
  const AuthState({
    required this.status,
    this.user,
    this.token,
    this.serverUrl,
  });

  const AuthState.initial() : this(status: AuthStatus.initial);

  const AuthState.authenticated({
    required User user,
    required String token,
    required String serverUrl,
  }) : this(
         status: AuthStatus.authenticated,
         user: user,
         token: token,
         serverUrl: serverUrl,
       );

  const AuthState.unauthenticated({String? serverUrl})
    : this(status: AuthStatus.unauthenticated, serverUrl: serverUrl);

  final AuthStatus status;
  final User? user;
  final String? token;
  final String? serverUrl;
}

final apiClientProvider = Provider<ApiClient>((ref) {
  final client = ApiClient();
  ref.onDispose(client.dispose);
  return client;
});

final authApiProvider = Provider<AuthApi>((ref) {
  return AuthApi(ref.watch(apiClientProvider));
});

final sharedPreferencesProvider = Provider<SharedPreferences>((ref) {
  throw StateError('SharedPreferences 未注入');
});

final authProvider = NotifierProvider<AuthNotifier, AuthState>(
  AuthNotifier.new,
);

class AuthNotifier extends Notifier<AuthState> {
  late final ApiClient _apiClient;
  late final AuthApi _authApi;
  late final SharedPreferences _preferences;
  Future<void>? _initFuture;

  @override
  AuthState build() {
    _apiClient = ref.watch(apiClientProvider);
    _authApi = ref.watch(authApiProvider);
    _preferences = ref.watch(sharedPreferencesProvider);
    _apiClient.onUnauthorized = onUnauthorized;
    return const AuthState.initial();
  }

  Future<void> init() async {
    final running = _initFuture;
    if (running != null) {
      return running;
    }

    final future = _restoreSession();
    _initFuture = future;
    try {
      await future;
    } finally {
      if (identical(_initFuture, future)) {
        _initFuture = null;
      }
    }
  }

  Future<void> _restoreSession() async {
    final storedServerUrl = _preferences.getString(fanWebServerUrlKey);
    final storedToken = _preferences.getString(fanWebTokenKey);

    if (storedServerUrl == null ||
        storedServerUrl.isEmpty ||
        storedToken == null ||
        storedToken.isEmpty) {
      _apiClient.setToken(null);
      state = AuthState.unauthenticated(serverUrl: storedServerUrl);
      return;
    }

    try {
      final serverUrl = ApiClient.normalizeServerUrl(storedServerUrl);
      _apiClient.configure(serverUrl);
      _apiClient.setToken(storedToken);
      final user = await _authApi.getCurrentUser();
      state = AuthState.authenticated(
        user: user,
        token: storedToken,
        serverUrl: serverUrl,
      );
    } catch (_) {
      await _clearStoredToken();
      _apiClient.setToken(null);
      state = AuthState.unauthenticated(serverUrl: storedServerUrl);
    }
  }

  Future<void> login(String serverUrl, String username, String password) async {
    final normalizedServerUrl = ApiClient.normalizeServerUrl(serverUrl);
    _apiClient.configure(normalizedServerUrl);
    _apiClient.setToken(null);

    final reachable = await _authApi.checkHealth(normalizedServerUrl);
    if (!reachable) {
      throw const ApiException(-1, '无法连接服务器');
    }

    final result = await _authApi.login(username.trim(), password);
    await _preferences.setString(fanWebServerUrlKey, normalizedServerUrl);
    await _preferences.setString(fanWebTokenKey, result.token);
    _apiClient.setToken(result.token);
    state = AuthState.authenticated(
      user: result.user,
      token: result.token,
      serverUrl: normalizedServerUrl,
    );
  }

  Future<void> logout() async {
    try {
      if (state.token != null && state.token!.isNotEmpty) {
        await _authApi.logout();
      }
    } catch (_) {
      // 网络异常不应阻塞本地登出。
    } finally {
      await _clearStoredToken();
      _apiClient.setToken(null);
      state = AuthState.unauthenticated(serverUrl: state.serverUrl);
    }
  }

  void onUnauthorized() {
    _apiClient.setToken(null);
    unawaited(_clearStoredToken());
    state = AuthState.unauthenticated(serverUrl: state.serverUrl);
  }

  Future<void> _clearStoredToken() async {
    await _preferences.remove(fanWebTokenKey);
  }
}
