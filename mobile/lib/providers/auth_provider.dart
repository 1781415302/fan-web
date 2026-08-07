import 'dart:async';
import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../api/api_client.dart';
import '../api/auth_api.dart';
import '../models/user.dart';
import '../services/progress_outbox.dart';

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
    } on DioException catch (_) {
      // 网络错误（超时、连接失败）：保留 token，进入降级状态
      final snapshot = _loadUserSnapshot();
      if (snapshot != null) {
        state = AuthState.authenticated(
          user: snapshot,
          token: storedToken,
          serverUrl: storedServerUrl,
          isSessionDegraded: true,
        );
      } else {
        // 没有用户快照，无法进入降级状态，回退到未认证
        state = AuthState.unauthenticated(serverUrl: storedServerUrl);
      }
    } catch (_) {
      // ApiException（401/2001 等）：token 确实失效
      await _clearStoredToken();
      _apiClient.setToken(null);
      state = AuthState.unauthenticated(serverUrl: storedServerUrl);
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
      state = state.copyWith(
        user: user,
        isSessionDegraded: false,
      );
    } catch (_) {
      // 仍然失败，保持降级状态
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
  }

  Future<void> logout() async {
    try {
      if (state.token != null && state.token!.isNotEmpty) {
        await _authApi.logout();
      }
    } catch (_) {
      // 网络异常不应阻塞本地登出。
    } finally {
      final serverUrl = state.serverUrl;
      final userId = state.user?.id;
      await _clearStoredToken();
      await _clearUserSnapshot();
      _apiClient.setToken(null);
      if (userId != null) {
        await _clearProgressOutbox(userId, serverUrl);
      }
      state = AuthState.unauthenticated(serverUrl: serverUrl);
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

  Future<void> _saveUserSnapshot(User user) async {
    try {
      _preferences.setString(fanWebUserSnapshotKey, jsonEncode(user.toJson()));
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
