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
    // 请求级已带 Authorization 时不覆盖：outbox 补报会为单次上报传入
    // 独立票据，不能让全局会话票据（可能已空）把它顶掉。
    final existing = options.headers['Authorization'];
    if (_token != null && _token!.isNotEmpty && existing == null) {
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
    // _onResponse 里 reject 的响应已经处理过未授权，且它的 error 一定是本类
    // 构造的 ApiException；reject 后会再次进入此处，必须跳过，否则一次未授权
    // 响应会触发两次回调（重复弹窗 / 重复 push 登录路由）。
    if (error.error is ApiException) {
      handler.next(error);
      return;
    }
    // Dio 默认只放行 2xx：真 HTTP 401 不会进 _onResponse，只会到这里。
    // 反代或旧服务返回的裸 401（以及 401 包着业务码 2001 的响应）同样必须
    // 触发 onUnauthorized，否则用户会一直卡在已失效的会话里。
    final response = error.response;
    if (response?.statusCode == 401 || _isUnauthorizedEnvelope(response?.data)) {
      _notifyUnauthorized();
    }
    handler.next(error);
  }

  // 响应体仍是未展开的信封（code/message/data）时为 true，用于识别裸 401
  // 上携带的业务码 2001。
  static bool _isUnauthorizedEnvelope(Object? data) {
    if (data is! Map) {
      return false;
    }
    final code = data['code'];
    return code is num && code.toInt() == 2001;
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
