import 'dart:async';
import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../api/api_client.dart';
import '../api/auth_api.dart';
import '../models/user.dart';
import '../services/progress_outbox.dart';
import 'anime_provider.dart';

const fanWebTokenKey = 'fan_web_token';
const fanWebServerUrlKey = 'fan_web_server_url';
const fanWebUserSnapshotKey = 'fan_web_user_snapshot';

enum AuthStatus { initial, authenticated, unauthenticated }

class AuthState {
  const AuthState({
    required this.status,
    this.user,
    this.token,
    this.serverUrl,
    this.isSessionDegraded = false,
  });

  const AuthState.initial() : this(status: AuthStatus.initial);

  const AuthState.authenticated({
    required User user,
    required String token,
    required String serverUrl,
    bool isSessionDegraded = false,
  }) : this(
         status: AuthStatus.authenticated,
         user: user,
         token: token,
         serverUrl: serverUrl,
         isSessionDegraded: isSessionDegraded,
       );

  const AuthState.unauthenticated({String? serverUrl})
    : this(status: AuthStatus.unauthenticated, serverUrl: serverUrl);

  final AuthStatus status;
  final User? user;
  final String? token;
  final String? serverUrl;
  final bool isSessionDegraded;

  AuthState copyWith({
    AuthStatus? status,
    User? user,
    String? token,
    String? serverUrl,
    bool? isSessionDegraded,
  }) {
    return AuthState(
      status: status ?? this.status,
      user: user ?? this.user,
      token: token ?? this.token,
      serverUrl: serverUrl ?? this.serverUrl,
      isSessionDegraded: isSessionDegraded ?? this.isSessionDegraded,
    );
  }
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
      await _saveUserSnapshot(user);
      state = AuthState.authenticated(
        user: user,
        token: storedToken,
        serverUrl: serverUrl,
      );
      unawaited(_syncOutbox(serverUrl, user.id, storedToken));
    } catch (error) {
      if (_isUnauthorizedError(error)) {
        await _invalidateSession(storedServerUrl);
      } else {
        _enterDegradedSession(serverUrl: storedServerUrl, token: storedToken);
      }
    }
  }

  /// 尝试恢复会话（从降级状态回到正常）。网络恢复后调用。
  Future<void> retrySession() async {
    if (state.status != AuthStatus.authenticated || !state.isSessionDegraded) {
      return;
    }
    try {
      final user = await _authApi.getCurrentUser();
      await _saveUserSnapshot(user);
      state = state.copyWith(user: user, isSessionDegraded: false);
      // 会话恢复成功后同步 outbox
      final serverUrl = state.serverUrl;
      final token = state.token;
      if (serverUrl != null && token != null && token.isNotEmpty) {
        final outbox = ref.read(progressOutboxProvider);
        unawaited(outbox.syncAll(serverUrl, user.id, token));
      }
    } catch (error) {
      if (_isUnauthorizedError(error)) {
        await _invalidateSession(state.serverUrl);
      }
      // 非认证错误保留 token 和降级状态，等待下次恢复。
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
    await _saveUserSnapshot(result.user);
    _apiClient.setToken(result.token);
    state = AuthState.authenticated(
      user: result.user,
      token: result.token,
      serverUrl: normalizedServerUrl,
    );
    unawaited(_syncOutbox(normalizedServerUrl, result.user.id, result.token));
  }

  void _syncOutbox(String serverUrl, int userId, String token) {
    if (token.isEmpty) {
      return;
    }
    try {
      final outbox = ref.read(progressOutboxProvider);
      unawaited(outbox.syncAll(serverUrl, userId, token));
    } catch (_) {}
  }

  Future<void> logout() async {
    // 在发起请求前捕获用户身份，避免登出请求触发 2001/401 级联
    // onUnauthorized 清空 state.user 后，finally 中无法再取到 userId。
    final userId = state.user?.id;
    final serverUrl = state.serverUrl;
    try {
      if (state.token != null && state.token!.isNotEmpty) {
        await _authApi.logout();
      }
    } catch (_) {
      // 网络异常不应阻塞本地登出。
    } finally {
      await _clearStoredToken();
      await _clearUserSnapshot();
      _apiClient.setToken(null);
      if (userId != null) {
        await _clearProgressOutbox(userId, serverUrl);
      }
      // 清除番剧列表缓存
      try {
        await ref.read(animeListProvider.notifier).clearCache();
      } catch (_) {}
      state = AuthState.unauthenticated(serverUrl: serverUrl);
    }
  }

  void onUnauthorized() {
    _apiClient.setToken(null);
    unawaited(_clearStoredToken());
    state = AuthState.unauthenticated(serverUrl: state.serverUrl);
  }

  bool _isUnauthorizedError(Object error) {
    if (error is ApiException) {
      return error.code == 2001;
    }
    if (error is DioException) {
      if (error.response?.statusCode == 401) {
        return true;
      }
      final nestedError = error.error;
      return nestedError is ApiException && nestedError.code == 2001;
    }
    return false;
  }

  Future<void> _invalidateSession(String? serverUrl) async {
    await _clearStoredToken();
    _apiClient.setToken(null);
    state = AuthState.unauthenticated(serverUrl: serverUrl);
  }

  void _enterDegradedSession({
    required String serverUrl,
    required String token,
  }) {
    _apiClient.setToken(token);
    final snapshot = _loadUserSnapshot();
    if (snapshot == null) {
      state = AuthState.unauthenticated(serverUrl: serverUrl);
      return;
    }
    state = AuthState.authenticated(
      user: snapshot,
      token: token,
      serverUrl: serverUrl,
      isSessionDegraded: true,
    );
  }

  Future<void> _clearStoredToken() async {
    await _preferences.remove(fanWebTokenKey);
  }

  Future<void> _saveUserSnapshot(User user) async {
    try {
      await _preferences.setString(
        fanWebUserSnapshotKey,
        jsonEncode(user.toJson()),
      );
    } catch (_) {}
  }

  User? _loadUserSnapshot() {
    try {
      final json = _preferences.getString(fanWebUserSnapshotKey);
      if (json == null || json.isEmpty) return null;
      return User.fromJson(jsonDecode(json) as Map<String, dynamic>);
    } catch (_) {
      return null;
    }
  }

  Future<void> _clearUserSnapshot() async {
    await _preferences.remove(fanWebUserSnapshotKey);
  }

  Future<void> _clearProgressOutbox(int userId, String? serverUrl) async {
    // 委托给 ProgressOutbox 清理，避免循环依赖
    try {
      final outbox = ref.read(progressOutboxProvider);
      await outbox.clearForUser(userId, serverUrl);
    } catch (_) {}
  }
}
