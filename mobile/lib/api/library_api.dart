import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/auth_provider.dart';
import 'api_client.dart';

final libraryApiProvider = Provider<LibraryApi>((ref) {
  return LibraryApi(ref.watch(apiClientProvider));
});

class LibraryApi {
  LibraryApi(this._client);

  final ApiClient _client;

  Future<ScanJob> startScan() {
    return _request(() async {
      final response = await _client.dio.post<dynamic>('library/scan');
      return ScanJob.fromJson(_asJsonMap(response.data, '扫描作业'));
    });
  }

  Future<ScanJob> getScan() {
    return _request(() async {
      final response = await _client.dio.get<dynamic>('library/scan');
      return ScanJob.fromJson(_asJsonMap(response.data, '扫描作业'));
    });
  }

  Future<PaginatedUnidentified> listUnidentified({
    int page = 1,
    int pageSize = 50,
  }) {
    return _request(() async {
      final response = await _client.dio.get<dynamic>(
        'library/unidentified',
        queryParameters: <String, dynamic>{'page': page, 'page_size': pageSize},
      );
      return PaginatedUnidentified.fromJson(_asJsonMap(response.data, '未识别列表'));
    });
  }

  Future<List<String>> listDirs() {
    return _request(() async {
      final response = await _client.dio.get<dynamic>('library/dirs');
      final json = _asJsonMap(response.data, '目录列表');
      final raw = json['items'];
      if (raw is! List) {
        return const <String>[];
      }
      return raw
          .map((item) => item?.toString() ?? '')
          .where((item) => item.isNotEmpty)
          .toList(growable: false);
    });
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

class MatchCandidate {
  const MatchCandidate({
    required this.id,
    required this.name,
    required this.nameCn,
    required this.score,
  });

  factory MatchCandidate.fromJson(Map<String, dynamic> json) {
    return MatchCandidate(
      id: _asInt(json['id']),
      name: _asString(json['name']),
      nameCn: _asString(json['name_cn']),
      score: _asDouble(json['score']),
    );
  }

  final int id;
  final String name;
  final String nameCn;
  final double score;

  String get displayName {
    final cn = nameCn.trim();
    return cn.isNotEmpty ? cn : name;
  }
}

class UnidentifiedFile {
  const UnidentifiedFile({
    required this.fileName,
    required this.reason,
    required this.filePath,
    required this.candidates,
  });

  factory UnidentifiedFile.fromJson(Map<String, dynamic> json) {
    final raw = json['candidates'];
    final candidates = raw is List
        ? raw
              .whereType<Map>()
              .map(
                (item) =>
                    MatchCandidate.fromJson(Map<String, dynamic>.from(item)),
              )
              .toList(growable: false)
        : const <MatchCandidate>[];
    return UnidentifiedFile(
      fileName: _asString(json['file_name']),
      reason: _asString(json['reason']),
      filePath: _asString(json['file_path']),
      candidates: candidates,
    );
  }

  final String fileName;
  final String reason;
  final String filePath;
  final List<MatchCandidate> candidates;
}

class LibraryScanResult {
  const LibraryScanResult({
    required this.totalFiles,
    required this.skipped,
    required this.newAnimes,
    required this.newEpisodes,
    required this.unidentified,
  });

  factory LibraryScanResult.fromJson(Map<String, dynamic> json) {
    final raw = json['unidentified'];
    final unidentified = raw is List
        ? raw
              .whereType<Map>()
              .map(
                (item) =>
                    UnidentifiedFile.fromJson(Map<String, dynamic>.from(item)),
              )
              .toList(growable: false)
        : const <UnidentifiedFile>[];
    return LibraryScanResult(
      totalFiles: _asInt(json['total_files']),
      skipped: _asInt(json['skipped']),
      newAnimes: _asInt(json['new_animes']),
      newEpisodes: _asInt(json['new_episodes']),
      unidentified: unidentified,
    );
  }

  final int totalFiles;
  final int skipped;
  final int newAnimes;
  final int newEpisodes;
  final List<UnidentifiedFile> unidentified;
}

class ScanJob {
  const ScanJob({
    required this.state,
    this.startedAt,
    this.finishedAt,
    this.error,
    this.result,
  });

  factory ScanJob.fromJson(Map<String, dynamic> json) {
    final rawResult = json['result'];
    return ScanJob(
      state: _asString(json['state']),
      startedAt: json['started_at']?.toString(),
      finishedAt: json['finished_at']?.toString(),
      error: json['error']?.toString(),
      result: rawResult is Map
          ? LibraryScanResult.fromJson(Map<String, dynamic>.from(rawResult))
          : null,
    );
  }

  final String state;
  final String? startedAt;
  final String? finishedAt;
  final String? error;
  final LibraryScanResult? result;

  bool get isTerminal => state == 'done' || state == 'error';
  bool get isRunning => state == 'running';
}

class PaginatedUnidentified {
  const PaginatedUnidentified({
    required this.items,
    required this.total,
    required this.page,
    required this.pageSize,
  });

  factory PaginatedUnidentified.fromJson(Map<String, dynamic> json) {
    final raw = json['items'];
    final items = raw is List
        ? raw
              .whereType<Map>()
              .map(
                (item) =>
                    UnidentifiedFile.fromJson(Map<String, dynamic>.from(item)),
              )
              .toList(growable: false)
        : const <UnidentifiedFile>[];
    return PaginatedUnidentified(
      items: items,
      total: _asInt(json['total']),
      page: _asInt(json['page']),
      pageSize: _asInt(json['page_size']),
    );
  }

  final List<UnidentifiedFile> items;
  final int total;
  final int page;
  final int pageSize;
}

/// POST 后每 1s GET 直到 done|error。15min 停轮询。不新增存储键。
Future<ScanJob> pollLibraryScan({
  required LibraryApi api,
  Duration pollInterval = const Duration(seconds: 1),
  Duration timeout = const Duration(minutes: 15),
  Future<void> Function(Duration duration)? delay,
  DateTime Function()? clock,
  bool Function()? shouldStop,
  void Function(ScanJob job)? onUpdate,
}) async {
  final wait = delay ?? Future<void>.delayed;
  final now = clock ?? DateTime.now;
  final deadline = now().add(timeout);
  var job = await api.startScan();
  onUpdate?.call(job);
  while (!job.isTerminal) {
    if (shouldStop?.call() == true) {
      return job;
    }
    if (!now().isBefore(deadline)) {
      return job;
    }
    await wait(pollInterval);
    if (shouldStop?.call() == true) {
      return job;
    }
    job = await api.getScan();
    onUpdate?.call(job);
  }
  return job;
}

int _asInt(Object? value) {
  if (value is num) {
    return value.toInt();
  }
  return int.tryParse(value?.toString() ?? '') ?? 0;
}

double _asDouble(Object? value) {
  if (value is num) {
    return value.toDouble();
  }
  return double.tryParse(value?.toString() ?? '') ?? 0;
}

String _asString(Object? value) => value?.toString() ?? '';
