import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/api/api_client.dart';
import 'package:fan_web/utils/api_error.dart';

void main() {
  group('describeApiError', () {
    test('ApiException 2001 returns unauthorized message', () {
      expect(
        describeApiError(const ApiException(2001, '未登录')),
        '登录状态已失效，请重新登录',
      );
    });

    test('ApiException other codes return original message', () {
      expect(
        describeApiError(const ApiException(2003, '用户名或密码错误')),
        '用户名或密码错误',
      );
    });

    test('DioException connectionTimeout returns timeout message', () {
      final error = DioException(
        requestOptions: RequestOptions(path: '/'),
        type: DioExceptionType.connectionTimeout,
      );
      expect(describeApiError(error), '连接服务器超时，请检查网络');
    });

    test('DioException receiveTimeout returns response timeout message', () {
      final error = DioException(
        requestOptions: RequestOptions(path: '/'),
        type: DioExceptionType.receiveTimeout,
      );
      expect(describeApiError(error), '服务器响应超时，请稍后重试');
    });

    test('DioException connectionError returns cannot connect message', () {
      final error = DioException(
        requestOptions: RequestOptions(path: '/'),
        type: DioExceptionType.connectionError,
      );
      expect(describeApiError(error), '无法连接服务器，请检查地址或网络');
    });

    test('DioException with nested ApiException extracts message', () {
      final error = DioException(
        requestOptions: RequestOptions(path: '/'),
        type: DioExceptionType.badResponse,
        error: const ApiException(2003, '用户名或密码错误'),
      );
      expect(describeApiError(error), '用户名或密码错误');
    });

    test('DioException with nested ApiException 2001 returns unauthorized', () {
      final error = DioException(
        requestOptions: RequestOptions(path: '/'),
        type: DioExceptionType.badResponse,
        error: const ApiException(2001, '未登录'),
      );
      expect(describeApiError(error), '登录状态已失效，请重新登录');
    });

    test('FormatException returns its message', () {
      expect(
        describeApiError(const FormatException('服务器地址格式不正确')),
        '服务器地址格式不正确',
      );
    });

    test('Other errors return fallback message', () {
      expect(
        describeApiError(Exception('unexpected')),
        '加载失败，请稍后重试',
      );
    });
  });
}
