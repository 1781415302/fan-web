import 'dart:convert';

enum DownloadStatus { idle, queued, downloading, completed, failed }

/// 身份键：服务器 + 集数（文件内容与用户无关，故意不含 userId）。
/// serverUrl 必须是规范化后的非空字符串（调用方负责解析）。
class DownloadKey {
  const DownloadKey(this.serverUrl, this.episodeId);

  final String serverUrl;
  final int episodeId;

  @override
  bool operator ==(Object other) {
    return other is DownloadKey &&
        other.serverUrl == serverUrl &&
        other.episodeId == episodeId;
  }

  @override
  int get hashCode => Object.hash(serverUrl, episodeId);

  @override
  String toString() => 'DownloadKey($serverUrl, $episodeId)';
}

/// UI 状态（仅存内存，由 Notifier 维护）。idle = 无任务无记录。
class DownloadTaskState {
  const DownloadTaskState({
    required this.status,
    this.receivedBytes = 0,
    this.totalBytes = 0,
    this.errorMessage,
  });

  final DownloadStatus status;
  // downloading 时已收总字节（含 .part 已有偏移）。
  final int receivedBytes;
  // 文件总字节；0 = 未知（响应未给出总长）。
  final int totalBytes;
  final String? errorMessage;
}

/// 已完成下载的持久化记录（index.json 的 records 数组元素）。
class DownloadRecord {
  const DownloadRecord({
    required this.serverUrl,
    required this.animeId,
    required this.episodeId,
    required this.epNumber,
    required this.animeTitle,
    required this.fileName,
    required this.fileSize,
    required this.completedAt,
  });

  final String serverUrl;
  final int animeId;
  final int episodeId;
  final int epNumber;
  final String animeTitle;
  // 落盘文件名，形如 "9_23.mp4"。
  final String fileName;
  // 实际字节数。
  final int fileSize;
  // ISO8601。
  final String completedAt;

  Map<String, dynamic> toJson() => {
    'server_url': serverUrl,
    'anime_id': animeId,
    'episode_id': episodeId,
    'ep_number': epNumber,
    'anime_title': animeTitle,
    'file_name': fileName,
    'file_size': fileSize,
    'completed_at': completedAt,
  };

  factory DownloadRecord.fromJson(Map<String, dynamic> json) {
    return DownloadRecord(
      serverUrl: json['server_url']?.toString() ?? '',
      animeId: (json['anime_id'] as num?)?.toInt() ?? 0,
      episodeId: (json['episode_id'] as num?)?.toInt() ?? 0,
      epNumber: (json['ep_number'] as num?)?.toInt() ?? 0,
      animeTitle: json['anime_title']?.toString() ?? '',
      fileName: json['file_name']?.toString() ?? '',
      fileSize: (json['file_size'] as num?)?.toInt() ?? 0,
      completedAt: json['completed_at']?.toString() ?? '',
    );
  }
}

/// index.json 编解码：{"version":1,"records":[<DownloadRecord.toJson>...]}
class DownloadIndexCodec {
  static const int version = 1;

  static String encode(List<DownloadRecord> records) {
    return jsonEncode({
      'version': version,
      'records': [for (final r in records) r.toJson()],
    });
  }

  // 损坏输入返回空列表（与 outbox 容错一致）。
  static List<DownloadRecord> decode(String raw) {
    try {
      final decoded = jsonDecode(raw);
      if (decoded is! Map) return const [];
      final list = decoded['records'];
      if (list is! List) return const [];
      final out = <DownloadRecord>[];
      for (final item in list) {
        try {
          if (item is Map<String, dynamic>) {
            out.add(DownloadRecord.fromJson(item));
          } else if (item is Map) {
            out.add(
              DownloadRecord.fromJson(Map<String, dynamic>.from(item)),
            );
          }
        } catch (_) {
          // 单条损坏跳过，不影响其余记录。
        }
      }
      return out;
    } catch (_) {
      return const [];
    }
  }
}
