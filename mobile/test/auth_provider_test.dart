import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:fan_web/api/api_client.dart';
import 'package:fan_web/api/auth_api.dart';
import 'package:fan_web/models/user.dart';
import 'package:fan_web/providers/auth_provider.dart';
import 'package:fan_web/services/progress_outbox.dart';

void main() {
  const serverUrl = 'http://192.168.1.10:8080';
  const token = 'saved-token';
  const snapshot = User(
    id: 7,
    username: 'tester',
    isAdmin: false,
    createdAt: '2026-08-01T00:00:00Z',
  );

  late SharedPreferences preferences;
  late ApiClient apiClient;
  late _StubAuthApi authApi;
  late ProviderContainer container;
  late _RecordingOutbox recordingOutbox;

  setUp(() async {
    SharedPreferences.setMockInitialValues({
      fanWebServerUrlKey: serverUrl,
      fanWebTokenKey: token,
      fanWebUserSnapshotKey: jsonEncode(snapshot.toJson()),
    });
    preferences = await SharedPreferences.getInstance();
    apiClient = ApiClient();
    authApi = _StubAuthApi(apiClient)..currentUser = snapshot;
    container = ProviderContainer(
      overrides: [
        sharedPreferencesProvider.overrideWithValue(preferences),
        apiClientProvider.overrideWithValue(apiClient),
        authApiProvider.overrideWithValue(authApi),
        progressOutboxProvider.overrideWith((ref) {
          recordingOutbox = _RecordingOutbox(ref);
          return recordingOutbox;
        }),
      ],
    );
  });

  tearDown(() {
    container.dispose();
    apiClient.dispose();
  });

  Future<AuthState> restoreWith(Object error) async {
    authApi.currentUserError = error;
    final notifier = container.read(authProvider.notifier);
    await notifier.init();
    return container.read(authProvider);
  }

  void expectDegradedSession(AuthState state) {
    expect(state.status, AuthStatus.authenticated);
    expect(state.isSessionDegraded, isTrue);
    expect(state.token, token);
    expect(state.user?.id, snapshot.id);
    expect(preferences.getString(fanWebTokenKey), token);
  }

  group('AuthNotifier.restoreSession', () {
    test(
      'connection timeout preserves token and enters degraded state',
      () async {
        final state = await restoreWith(
          _dioError(DioExceptionType.connectionTimeout),
        );

        expectDegradedSession(state);
      },
    );

    test(
      'connection failure preserves token and enters degraded state',
      () async {
        final state = await restoreWith(
          _dioError(DioExceptionType.connectionError),
        );

        expectDegradedSession(state);
      },
    );

    test('HTTP 500 preserves token and enters degraded state', () async {
      final state = await restoreWith(
        _dioError(DioExceptionType.badResponse, statusCode: 500),
      );

      expectDegradedSession(state);
    });

    test('non-authentication business error preserves token', () async {
      final state = await restoreWith(const ApiException(9999, '服务器响应格式错误'));

      expectDegradedSession(state);
    });

    test('HTTP 401 clears token', () async {
      final state = await restoreWith(
        _dioError(DioExceptionType.badResponse, statusCode: 401),
      );

      expect(state.status, AuthStatus.unauthenticated);
      expect(state.serverUrl, serverUrl);
      expect(preferences.getString(fanWebTokenKey), isNull);
    });

    test('business code 2001 clears token', () async {
      final state = await restoreWith(const ApiException(2001, '登录状态已失效'));

      expect(state.status, AuthStatus.unauthenticated);
      expect(state.serverUrl, serverUrl);
      expect(preferences.getString(fanWebTokenKey), isNull);
    });
  });

  test(
    'degraded session returns to authenticated after retry succeeds',
    () async {
      final notifier = container.read(authProvider.notifier);
      authApi.currentUserError = _dioError(DioExceptionType.connectionError);
      await notifier.init();
      expectDegradedSession(container.read(authProvider));

      authApi.currentUserError = null;
      await notifier.retrySession();
      await Future<void>.delayed(Duration.zero);

      final state = container.read(authProvider);
      expect(state.status, AuthStatus.authenticated);
      expect(state.isSessionDegraded, isFalse);
      expect(state.user?.id, snapshot.id);
    },
  );

  test('unauthorized retry clears a degraded session', () async {
    final notifier = container.read(authProvider.notifier);
    authApi.currentUserError = _dioError(DioExceptionType.connectionError);
    await notifier.init();

    authApi.currentUserError = const ApiException(2001, '登录状态已失效');
    await notifier.retrySession();

    expect(container.read(authProvider).status, AuthStatus.unauthenticated);
    expect(preferences.getString(fanWebTokenKey), isNull);
  });

  test('successful restoreSession syncs outbox', () async {
    final notifier = container.read(authProvider.notifier);
    await notifier.init();
    await Future<void>.delayed(Duration.zero);
    expect(container.read(authProvider).isSessionDegraded, isFalse);
    expect(recordingOutbox.syncCalls, 1);
  });

  test('degraded restoreSession does not sync outbox', () async {
    await restoreWith(_dioError(DioExceptionType.connectionTimeout));
    await Future<void>.delayed(Duration.zero);
    expect(recordingOutbox.syncCalls, 0);
  });

  test('login syncs outbox', () async {
    SharedPreferences.setMockInitialValues({});
    preferences = await SharedPreferences.getInstance();
    container.dispose();
    container = ProviderContainer(
      overrides: [
        sharedPreferencesProvider.overrideWithValue(preferences),
        apiClientProvider.overrideWithValue(apiClient),
        authApiProvider.overrideWithValue(authApi),
        progressOutboxProvider.overrideWith((ref) {
          recordingOutbox = _RecordingOutbox(ref);
          return recordingOutbox;
        }),
      ],
    );
    final notifier = container.read(authProvider.notifier);
    await notifier.login(serverUrl, 'tester', 'secret');
    await Future<void>.delayed(Duration.zero);
    expect(recordingOutbox.syncCalls, 1);
  });

  test('logout clears user data and outbox but preserves server URL', () async {
    final notifier = container.read(authProvider.notifier);
    await notifier.init();
    final outbox = container.read(progressOutboxProvider);
    await outbox.save(
      const PendingProgress(
        serverUrl: serverUrl,
        userId: 7,
        episodeId: 11,
        position: 120,
        watched: false,
        updatedAt: '2026-08-07T12:00:00Z',
      ),
    );

    await notifier.logout();

    expect(container.read(authProvider).status, AuthStatus.unauthenticated);
    expect(preferences.getString(fanWebServerUrlKey), serverUrl);
    expect(preferences.getString(fanWebTokenKey), isNull);
    expect(preferences.getString(fanWebUserSnapshotKey), isNull);
    expect(await outbox.getPending(serverUrl, snapshot.id), isEmpty);
  });
}

DioException _dioError(
  DioExceptionType type, {
  int? statusCode,
  Object? error,
}) {
  final request = RequestOptions(path: 'auth/me');
  return DioException(
    requestOptions: request,
    response: statusCode == null
        ? null
        : Response<dynamic>(requestOptions: request, statusCode: statusCode),
    type: type,
    error: error,
  );
}

class _StubAuthApi extends AuthApi {
  _StubAuthApi(super.client);

  late User currentUser;
  Object? currentUserError;

  @override
  Future<User> getCurrentUser() async {
    final error = currentUserError;
    if (error != null) {
      throw error;
    }
    return currentUser;
  }

  @override
  Future<bool> checkHealth(String serverUrl) async => true;

  @override
  Future<LoginResponse> login(String username, String password) async {
    return LoginResponse(
      token: 'login-token',
      expiresAt: '2026-08-18T00:00:00Z',
      user: currentUser,
    );
  }

  @override
  Future<void> logout() async {}
}

class _RecordingOutbox extends ProgressOutbox {
  _RecordingOutbox(super.ref);

  int syncCalls = 0;

  @override
  Future<void> syncAll(String serverUrl, int userId, String token) async {
    syncCalls++;
  }
}
