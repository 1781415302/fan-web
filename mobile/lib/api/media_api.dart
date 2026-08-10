import 'dart:async';

import 'package:dio/dio.dart';

import 'api_client.dart';

/// 媒体票据接口访问。老服务器（v1.2.2/v1.2.3）没有
/// POST /api/episodes/:id/media-token，会返回业务码 404（接口不存在），
/// 此时应回落为旧 `token` query 播放 URL。
class MediaApi {
  MediaApi(this._client);

  final ApiClient _client;

  static const int notFoundCode = 404;

  /// 请求指定 episode 的短期媒体票据。
  /// 老服务器返回 business code 404 时抛出 [MediaTokenUnsupported]，调用方应回退旧 URL。
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
  final normalized = serverUrl.trim().replaceFirst(RegExp(r'/+$'), '');
  return '$normalized/api/episodes/$episodeId/stream?media_token=${Uri.encodeComponent(mediaToken)}';
}

String buildLegacyStreamUrl(String serverUrl, int episodeId, String loginToken) {
  final normalized = serverUrl.trim().replaceFirst(RegExp(r'/+$'), '');
  return '$normalized/api/episodes/$episodeId/stream?token=${Uri.encodeComponent(loginToken)}';
}