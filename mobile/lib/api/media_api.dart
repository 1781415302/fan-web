import 'dart:async';

import 'package:dio/dio.dart';

import 'api_client.dart';

/// 媒体票据接口访问。
class MediaApi {
  MediaApi(this._client);

  final ApiClient _client;

  static const int notFoundCode = 404;

  /// 请求指定 episode 的短期媒体票据。
  /// 老服务器返回 HTTP 404 或 business code 404 时抛出 [MediaTokenUnsupported]。
  Future<MediaTokenResult> fetchMediaToken(int episodeId) async {
    try {
      final response = await _client.dio.post<dynamic>(
        'episodes/$episodeId/media-token',
      );
      final data = response.data;
      if (data is! Map) {
        throw const FormatException('媒体票据响应格式错误');
      }
      final token = data['token']?.toString();
      final expiresAt = data['expires_at']?.toString();
      if (token == null || token.isEmpty) {
        throw const FormatException('媒体票据响应缺少 token');
      }
      return MediaTokenResult(token: token, expiresAt: expiresAt ?? '');
    } on DioException catch (e) {
      final status = e.response?.statusCode;
      if (status == notFoundCode) {
        throw const MediaTokenUnsupported();
      }
      final nested = e.error;
      if (nested is ApiException) {
        if (nested.code == notFoundCode) {
          throw const MediaTokenUnsupported();
        }
        throw nested;
      }
      rethrow;
    }
  }
}

/// 媒体票据接口不支持的信号（老服务器）。
class MediaTokenUnsupported implements Exception {
  const MediaTokenUnsupported();
}

class MediaTokenResult {
  const MediaTokenResult({required this.token, this.expiresAt = ''});

  final String token;
  final String expiresAt;
}

/// 旧的登录 JWT 也接入到 apiClient；当前 client 内部已用 Bearer。
/// 此处保留可测试的解析函数。
String buildStreamUrlWithMediaToken(String serverUrl, int episodeId, String mediaToken) {
  var normalized = serverUrl.trim().replaceFirst(RegExp(r'/+$'), '');
  // 兜底补全协议头，避免传入无 scheme 的 serverUrl 时拼接出非法播放地址
  // （与 ApiClient.normalizeServerUrl 的口径一致；正常调用方已传规范化 URL）。
  if (!RegExp(r'^https?://', caseSensitive: false).hasMatch(normalized)) {
    normalized = 'http://$normalized';
  }
  return '$normalized/api/episodes/$episodeId/stream?media_token=${Uri.encodeComponent(mediaToken)}';
}
