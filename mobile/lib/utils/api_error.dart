import 'package:dio/dio.dart';

import '../api/api_client.dart';

/// 统一的 API 错误文案映射。按错误类型给出用户可读的中文提示。
String describeApiError(Object error) {
  // 嵌套提取：DioException.error 可能是 ApiException
  if (error is DioException) {
    final nested = error.error;
    if (nested is ApiException) {
      return _apiExceptionMessage(nested);
    }
    return switch (error.type) {
      DioExceptionType.connectionTimeout => '连接服务器超时，请检查网络',
      DioExceptionType.sendTimeout ||
      DioExceptionType.receiveTimeout => '服务器响应超时，请稍后重试',
      DioExceptionType.connectionError => '无法连接服务器，请检查地址或网络',
      _ => '网络请求失败，请检查服务器连接',
    };
  }
  if (error is ApiException) {
    return _apiExceptionMessage(error);
  }
  if (error is FormatException) {
    return error.message;
  }
  return '加载失败，请稍后重试';
}

String _apiExceptionMessage(ApiException error) {
  if (error.code == 2001) {
    return '登录状态已失效，请重新登录';
  }
  return error.message;
}
