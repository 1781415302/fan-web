import 'package:dio/dio.dart';

import '../models/user.dart';
import 'api_client.dart';

class AuthApi {
  AuthApi(this._client);

  final ApiClient _client;

  Future<LoginResponse> login(String username, String password) {
    return _request(() async {
      final response = await _client.dio.post<dynamic>(
        'auth/login',
        data: <String, dynamic>{'username': username, 'password': password},
      );
      return LoginResponse.fromJson(_asJsonMap(response.data, '登录响应'));
    });
  }

  Future<User> getCurrentUser() {
    return _request(() async {
      final response = await _client.dio.get<dynamic>('auth/me');
      return User.fromJson(_asJsonMap(response.data, '用户响应'));
    });
  }

  Future<void> logout() {
    return _request(() async {
      await _client.dio.post<dynamic>('auth/logout');
    });
  }

  Future<bool> checkHealth(String serverUrl) async {
    try {
      final normalized = ApiClient.normalizeServerUrl(serverUrl);
      final dio = Dio(
        BaseOptions(
          baseUrl: '$normalized/api/',
          connectTimeout: const Duration(seconds: 10),
          receiveTimeout: const Duration(seconds: 10),
        ),
      );
      final response = await dio.get<dynamic>('health');
      final data = response.data;
      return response.statusCode == 200 &&
          data is Map &&
          data['code'] is num &&
          (data['code'] as num).toInt() == 0 &&
          data['data'] is Map &&
          data['data']['status'] == 'ok';
    } catch (_) {
      return false;
    }
  }

  Future<T> _request<T>(Future<T> Function() request) async {
    try {
      return await request();
    } on DioException catch (error) {
      final apiError = error.error;
      if (apiError is ApiException) {
        throw apiError;
      }
      rethrow;
    }
  }

  Map<String, dynamic> _asJsonMap(Object? value, String label) {
    if (value is Map) {
      return Map<String, dynamic>.from(value);
    }
    throw FormatException('$label格式错误');
  }
}
