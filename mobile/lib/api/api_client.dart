import 'package:dio/dio.dart';

typedef UnauthorizedCallback = void Function();

class ApiException implements Exception {
  const ApiException(this.code, this.message);

  final int code;
  final String message;

  @override
  String toString() => message;
}

class ApiClient {
  ApiClient({Dio? dio})
    : _dio =
          dio ??
          Dio(
            BaseOptions(
              connectTimeout: const Duration(seconds: 10),
              receiveTimeout: const Duration(seconds: 10),
            ),
          ) {
    _dio.interceptors.add(
      InterceptorsWrapper(
        onRequest: _onRequest,
        onResponse: _onResponse,
        onError: _onError,
      ),
    );
  }

  final Dio _dio;
  String? _token;

  UnauthorizedCallback? onUnauthorized;

  Dio get dio => _dio;

  String? get baseUrl =>
      _dio.options.baseUrl.isEmpty ? null : _dio.options.baseUrl;

  void configure(String serverUrl) {
    final normalized = normalizeServerUrl(serverUrl);
    _dio.options.baseUrl = '$normalized/api/';
  }

  void setToken(String? token) {
    _token = token;
  }

  static String normalizeServerUrl(String serverUrl) {
    var normalized = serverUrl.trim();
    if (normalized.isEmpty) {
      throw const FormatException('请输入服务器地址');
    }
    if (!RegExp(r'^https?://', caseSensitive: false).hasMatch(normalized)) {
      normalized = 'http://$normalized';
    }
    normalized = normalized.replaceFirst(RegExp(r'/+$'), '');
    final uri = Uri.tryParse(normalized);
    if (uri == null || uri.host.isEmpty) {
      throw const FormatException('服务器地址格式不正确');
    }
    return normalized;
  }

  void _onRequest(RequestOptions options, RequestInterceptorHandler handler) {
    if (_token != null && _token!.isNotEmpty) {
      options.headers['Authorization'] = 'Bearer $_token';
    }
    handler.next(options);
  }

  void _onResponse(
    Response<dynamic> response,
    ResponseInterceptorHandler handler,
  ) {
    if (response.statusCode == 401) {
      _notifyUnauthorized();
      handler.reject(_errorFor(response, const ApiException(2001, '登录状态已失效')));
      return;
    }

    final envelope = response.data;
    if (envelope is! Map) {
      handler.reject(
        _errorFor(response, const ApiException(9999, '服务器响应格式错误')),
      );
      return;
    }

    final codeValue = envelope['code'];
    if (codeValue is! num) {
      handler.reject(
        _errorFor(response, const ApiException(9999, '服务器响应格式错误')),
      );
      return;
    }

    final code = codeValue.toInt();
    final message = envelope['message']?.toString() ?? '请求失败';
    if (code == 0) {
      response.data = envelope['data'];
      handler.next(response);
      return;
    }

    final exception = ApiException(code, message);
    if (code == 2001) {
      _notifyUnauthorized();
    }
    handler.reject(_errorFor(response, exception));
  }

  void _onError(DioException error, ErrorInterceptorHandler handler) {
    final response = error.response;
    if (response?.statusCode == 401 ||
        _isUnauthorizedEnvelope(response?.data)) {
      _notifyUnauthorized();
    }
    handler.next(error);
  }

  bool _isUnauthorizedEnvelope(Object? data) {
    return data is Map &&
        data['code'] is num &&
        (data['code'] as num).toInt() == 2001;
  }

  DioException _errorFor(Response<dynamic> response, ApiException exception) {
    return DioException(
      requestOptions: response.requestOptions,
      response: response,
      type: DioExceptionType.badResponse,
      error: exception,
      message: exception.message,
    );
  }

  void _notifyUnauthorized() {
    onUnauthorized?.call();
  }

  void dispose() {
    _dio.close(force: true);
  }
}
