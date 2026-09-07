import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/media_api.dart';
import '../models/anime.dart';
import '../models/download.dart';
import '../providers/auth_provider.dart';
import '../providers/download_provider.dart';

// 进度回调节流：两者满足其一即上报，避免每 chunk 重建状态 map。
const _progressBytesThreshold = 256 * 1024;
const _progressInterval = Duration(milliseconds: 200);

/// 从 episode.filePath 推导落盘扩展名（含点）。
/// 服务端已保证合法视频扩展名；取不到时回退 .mp4。
String downloadFileExtension(String filePath) {
  final name = filePath.split('/').last;
  final dot = name.lastIndexOf('.');
  if (dot < 0 || dot == name.length - 1) {
    return '.mp4';
  }
  return name.substring(dot);
}

/// 下载目录：`root/downloads/sha256(serverUrl) hex 前 16 位`。
String downloadDirFor(String root, String serverUrl) {
  final digest = sha256.convert(utf8.encode(serverUrl)).toString();
  return '$root/downloads/${digest.substring(0, 16)}';
}

bool _isJsonContentType(String? contentType) {
  return (contentType ?? '').toLowerCase().contains('application/json');
}

/// 解析 206 的 Content-Range 总长，如 "bytes 4-9/10" -> 10。
/// 取不到返回 null（调用方按未知总长处理）。
int? parseContentRangeTotal(String? value) {
  if (value == null) return null;
  final slash = value.lastIndexOf('/');
  if (slash < 0 || slash == value.length - 1) return null;
  return int.tryParse(value.substring(slash + 1).trim());
}

class _DownloadJob {
  _DownloadJob({
    required this.serverUrl,
    required this.episode,
    required this.animeTitle,
  });

  final String serverUrl;
  final Episode episode;
  final String animeTitle;
  final Completer<void> done = Completer<void>();

  DownloadKey get key => DownloadKey(serverUrl, episode.id);
}

/// `downloadsRoot/downloads/index.json` 的原子读写 + 串行链
/// （仿 ProgressOutbox._enqueueStorage）。
class DownloadIndexStore {
  DownloadIndexStore(this._ref);

  final Ref _ref;
  Future<void> _chain = Future<void>.value();

  Future<String> _indexPath() async {
    final root = await _ref.read(downloadsRootProvider.future);
    return '$root/downloads/index.json';
  }

  Future<List<DownloadRecord>> load() => _enqueue(_loadDirect);

  Future<void> upsert(DownloadRecord record) => _enqueue(() async {
    final all = await _loadDirect();
    all.removeWhere(
      (r) => r.serverUrl == record.serverUrl && r.episodeId == record.episodeId,
    );
    all.add(record);
    await _saveDirect(all);
  });

  Future<void> remove(String serverUrl, int episodeId) => _enqueue(() async {
    final all = await _loadDirect();
    all.removeWhere(
      (r) => r.serverUrl == serverUrl && r.episodeId == episodeId,
    );
    await _saveDirect(all);
  });

  Future<T> _enqueue<T>(Future<T> Function() operation) {
    final result = _chain.then((_) => operation());
    _chain = result.then<void>(
      (_) {},
      onError: (Object _, StackTrace _) {},
    );
    return result;
  }

  Future<List<DownloadRecord>> _loadDirect() async {
    try {
      final file = File(await _indexPath());
      if (!await file.exists()) return [];
      return DownloadIndexCodec.decode(await file.readAsString());
    } catch (_) {
      // 索引损坏按空处理，下次写入时覆盖。
      return [];
    }
  }

  Future<void> _saveDirect(List<DownloadRecord> all) async {
    try {
      final path = await _indexPath();
      final file = File(path);
      await file.parent.create(recursive: true);
      // 临时文件 + rename 原子替换，避免半写损坏。
      final tmp = File('$path.tmp');
      await tmp.writeAsString(DownloadIndexCodec.encode(all), flush: true);
      try {
        if (await file.exists()) {
          await file.delete();
        }
      } catch (_) {
        // 旧文件删除失败不阻塞，后续 rename 会覆盖（同盘）。
      }
      await tmp.rename(path);
    } catch (_) {
      // 磁盘满等写入失败由调用方按 failed 处理，此处不抛。
    }
  }
}

/// 移动端下载引擎：串行队列 + dio 下载 + 断点续传 + 卡死检测。
/// 不 import 任何 widget/screen/player；索引走文件（不用 SharedPreferences）。
class EpisodeDownloader {
  EpisodeDownloader(
    this._ref, {
    Dio? dio,
    this.stallTimeout = const Duration(seconds: 60),
    this.onStateChanged,
  }) : _dio = dio ?? _createDio();

  final Ref _ref;
  final Dio _dio;

  /// 相邻数据块最大允许间隔；receiveTimeout 置 null，不依赖其语义。
  /// 测试可注入短超时覆盖卡死分支。
  final Duration stallTimeout;

  /// 状态推送回调（由 downloadProvider 注入，写 state 前判 disposed）。
  void Function(DownloadKey key, DownloadTaskState state)? onStateChanged;

  final List<_DownloadJob> _pending = [];
  _DownloadJob? _active;
  CancelToken? _activeToken;
  final Map<DownloadKey, DownloadRecord> _completed = {};
  // 已知落盘文件名（含失败残留的 .part 定位），key -> "9_23.mp4"。
  final Map<DownloadKey, String> _fileNames = {};
  String? _cachedRoot;
  bool _disposed = false;
  late final DownloadIndexStore _store = DownloadIndexStore(_ref);

  static Dio _createDio() {
    // 独立 Dio 实例下载（仿 app_updater 先例），绝不写回全局 ApiClient。
    return Dio(
      BaseOptions(
        connectTimeout: const Duration(seconds: 10),
        // 卡死由显式计时器判定，此处置 null。
        receiveTimeout: null,
        validateStatus: (status) => status != null && status < 500,
      ),
    );
  }

  List<DownloadRecord> get completedRecords =>
      List.unmodifiable(_completed.values);

  void dispose() {
    _disposed = true;
    try {
      _activeToken?.cancel('dispose');
    } catch (_) {
      // 取消失败不阻塞释放。
    }
    try {
      _dio.close();
    } catch (_) {
      // 关闭失败不阻塞释放。
    }
  }

  /// 幂等：queued/downloading 中或已有 completed 记录时 no-op；
  /// failed/idle 时入队并 pump。返回的 Future 在本任务结束（完成/失败/
  /// 取消）后完成，便于测试等待；失败经状态回调表达，不抛异常。
  Future<void> enqueue({
    required String serverUrl,
    required Episode episode,
    required String animeTitle,
  }) {
    final job = _DownloadJob(
      serverUrl: serverUrl,
      episode: episode,
      animeTitle: animeTitle,
    );
    if (_disposed) {
      return Future.value();
    }
    if (_completed.containsKey(job.key)) {
      return Future.value();
    }
    if (_active?.key == job.key ||
        _pending.any((j) => j.key == job.key)) {
      return Future.value();
    }
    _pending.add(job);
    _emit(job.key, const DownloadTaskState(status: DownloadStatus.queued));
    _pump();
    return job.done.future;
  }

  /// 取消活动/排队任务；.part 保留（重试可续传）；状态回 idle。
  /// 已 completed 的记录不受影响（删除走 delete）。
  Future<void> cancel(String serverUrl, int episodeId) async {
    final key = DownloadKey(serverUrl, episodeId);
    final pendingIndex = _pending.indexWhere((j) => j.key == key);
    if (pendingIndex >= 0) {
      final job = _pending.removeAt(pendingIndex);
      if (!job.done.isCompleted) {
        job.done.complete();
      }
      _emit(key, const DownloadTaskState(status: DownloadStatus.idle));
      return;
    }
    if (_active?.key == key) {
      try {
        _activeToken?.cancel('user-cancel');
      } catch (_) {
        // 取消信令失败则等待任务自行结束。
      }
      try {
        await _active?.done.future.timeout(const Duration(seconds: 10));
      } catch (_) {
        // 等待超时也不阻塞调用方。
      }
      return;
    }
    // 非活动/排队任务（failed/completed/未知）不做状态改动。
  }

  /// 先 cancel，再删文件 + 索引；completed 也允许直接删；最终回 idle。
  Future<void> delete(String serverUrl, int episodeId) async {
    final key = DownloadKey(serverUrl, episodeId);
    final pendingIndex = _pending.indexWhere((j) => j.key == key);
    if (pendingIndex >= 0) {
      final job = _pending.removeAt(pendingIndex);
      if (!job.done.isCompleted) {
        job.done.complete();
      }
    }
    if (_active?.key == key) {
      try {
        _activeToken?.cancel('delete');
      } catch (_) {
        // 取消信令失败则继续删索引与残留文件。
      }
      try {
        await _active?.done.future.timeout(const Duration(seconds: 10));
      } catch (_) {
        // 等待超时也不阻塞删除。
      }
    }
    try {
      final root = _cachedRoot ?? await _resolveRoot();
      final dir = downloadDirFor(root, serverUrl);
      final record = _completed[key];
      final known = _fileNames[key];
      final names = <String>{
        if (record != null) record.fileName,
        if (known != null) ...[known, '$known.part'],
      };
      for (final name in names) {
        try {
          final file = File('$dir/$name');
          if (await file.exists()) {
            await file.delete();
          }
        } catch (_) {
          // 单个文件删除失败不阻塞其余清理。
        }
      }
      // 兜底：按 episodeId 精确匹配该集残留（含未知扩展名的 .part）。
      try {
        final directory = Directory(dir);
        if (await directory.exists()) {
          final pattern = RegExp('^\\d+_$episodeId\\..*(\\.part)?\$');
          await for (final entity in directory.list()) {
            if (entity is! File) continue;
            final name = entity.path.split('/').last;
            if (!pattern.hasMatch(name)) continue;
            try {
              await entity.delete();
            } catch (_) {
              // 单个残留删除失败不阻塞。
            }
          }
        }
      } catch (_) {
        // 目录遍历失败不阻塞索引清理。
      }
    } catch (_) {
      // 根目录解析失败时仍继续清理索引与内存态。
    }
    try {
      await _store.remove(serverUrl, episodeId);
    } catch (_) {
      // 索引删除失败不阻塞状态复位。
    }
    _completed.remove(key);
    _fileNames.remove(key);
    _emit(key, const DownloadTaskState(status: DownloadStatus.idle));
  }

  /// 加载 index.json、剔除文件已丢失的记录、删除所有孤儿 .part。
  /// 调用时队列为空，任何 .part 都无主。
  /// 注意已知限制：App 进后台可能被系统杀死；被杀后 .part 会在下次启动
  /// 被此处清理删除，重新点击下载将从头下载；已完成的下载不受影响。
  Future<void> restoreIndex() async {
    final records = await _store.load();
    String root;
    try {
      root = await _resolveRoot();
    } catch (_) {
      // 根目录不可用时无法校验，保留空内存态。
      return;
    }
    final valid = <DownloadKey, DownloadRecord>{};
    for (final record in records) {
      final path =
          '${downloadDirFor(root, record.serverUrl)}/${record.fileName}';
      bool exists = false;
      try {
        exists = File(path).existsSync();
      } catch (_) {
        exists = false;
      }
      if (exists) {
        valid[DownloadKey(record.serverUrl, record.episodeId)] = record;
      } else {
        try {
          await _store.remove(record.serverUrl, record.episodeId);
        } catch (_) {
          // 单条剔除失败不阻塞其余记录。
        }
      }
    }
    _completed
      ..clear()
      ..addAll(valid);
    // 孤儿 .part 清理。
    try {
      final downloadsDir = Directory('$root/downloads');
      if (await downloadsDir.exists()) {
        await for (final entity in downloadsDir.list(recursive: true)) {
          if (entity is! File || !entity.path.endsWith('.part')) continue;
          try {
            await entity.delete();
          } catch (_) {
            // 单个孤儿删除失败不阻塞。
          }
        }
      }
    } catch (_) {
      // 清理失败不阻塞启动。
    }
  }

  /// 同步：completed 记录且文件存在时返回绝对路径，否则 null。
  /// 冷启动到索引加载完成的极短窗口内会瞬时 miss（回落网络流），已接受。
  String? localFilePath(String serverUrl, int episodeId) {
    final root = _cachedRoot;
    if (root == null) return null;
    final key = DownloadKey(serverUrl, episodeId);
    final record = _completed[key];
    if (record == null) return null;
    final path = '${downloadDirFor(root, serverUrl)}/${record.fileName}';
    try {
      return File(path).existsSync() ? path : null;
    } catch (_) {
      return null;
    }
  }

  void _pump() {
    if (_disposed || _active != null || _pending.isEmpty) return;
    final job = _pending.removeAt(0);
    _active = job;
    unawaited(_runAndComplete(job));
  }

  Future<void> _runAndComplete(_DownloadJob job) async {
    try {
      await _runJob(job);
    } catch (_) {
      // _runJob 内部分支已覆盖所有已知失败；此处仅兜底未知异常。
      _emit(
        job.key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载失败，请重试',
        ),
      );
    } finally {
      _active = null;
      _activeToken = null;
      if (!job.done.isCompleted) {
        job.done.complete();
      }
      _pump();
    }
  }

  Future<void> _runJob(_DownloadJob job) async {
    final key = job.key;
    // token 每次 attempt 启动时从 authProvider 读当前值。
    String token = '';
    try {
      token = _ref.read(authProvider).token ?? '';
    } catch (_) {
      token = '';
    }
    if (token.isEmpty) {
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '请先登录',
        ),
      );
      return;
    }
    late final String root;
    try {
      root = await _resolveRoot();
    } catch (_) {
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载失败，请重试',
        ),
      );
      return;
    }
    final dir = downloadDirFor(root, job.serverUrl);
    try {
      await Directory(dir).create(recursive: true);
    } catch (_) {
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载失败，请重试',
        ),
      );
      return;
    }
    final ext = downloadFileExtension(job.episode.filePath);
    final fileName = '${job.episode.animeId}_${job.episode.id}$ext';
    _fileNames[key] = fileName;
    final targetPath = '$dir/$fileName';
    final partPath = '$targetPath.part';
    int resumeFrom = 0;
    try {
      final partFile = File(partPath);
      if (await partFile.exists()) {
        resumeFrom = await partFile.length();
      }
    } catch (_) {
      resumeFrom = 0;
    }
    await _attempt(
      job: job,
      targetPath: targetPath,
      partPath: partPath,
      resumeFrom: resumeFrom,
      token: token,
      retried: false,
    );
  }

  Future<void> _attempt({
    required _DownloadJob job,
    required String targetPath,
    required String partPath,
    required int resumeFrom,
    required String token,
    required bool retried,
  }) async {
    final key = job.key;
    final cancelToken = CancelToken();
    _activeToken = cancelToken;
    _emit(
      key,
      DownloadTaskState(
        status: DownloadStatus.downloading,
        receivedBytes: resumeFrom,
      ),
    );
    final headers = <String, String>{'Authorization': 'Bearer $token'};
    if (resumeFrom > 0) {
      headers['Range'] = 'bytes=$resumeFrom-';
    }
    final url = buildDownloadUrl(job.serverUrl, job.episode.id);
    late final Response<ResponseBody> response;
    try {
      response = await _dio.get<ResponseBody>(
        url,
        options: Options(
          responseType: ResponseType.stream,
          validateStatus: (status) => status != null && status < 500,
          headers: headers,
          // 卡死由显式计时器判定，不依赖 receiveTimeout 语义。
          receiveTimeout: null,
        ),
        cancelToken: cancelToken,
      );
    } on DioException catch (e) {
      if (CancelToken.isCancel(e) || cancelToken.isCancelled) {
        // 用户取消（含 delete）：状态回 idle，.part 保留。
        _emit(key, const DownloadTaskState(status: DownloadStatus.idle));
        return;
      }
      if (e.response?.statusCode == 401) {
        _emit(
          key,
          const DownloadTaskState(
            status: DownloadStatus.failed,
            errorMessage: '登录状态已失效',
          ),
        );
        return;
      }
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载失败，请重试',
        ),
      );
      return;
    } catch (_) {
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载失败，请重试',
        ),
      );
      return;
    }

    final status = response.statusCode ?? 0;
    final contentType =
        response.headers.value('content-type') ??
        response.headers.value(Headers.contentTypeHeader);
    final payload = response.data;
    if (payload == null) {
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载失败，请重试',
        ),
      );
      return;
    }

    if (status == 404) {
      await _drainBody(payload);
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '服务器不支持下载或文件不存在',
        ),
      );
      return;
    }
    if (status == 401) {
      await _drainBody(payload);
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '登录状态已失效',
        ),
      );
      return;
    }
    if (status == 416) {
      // .part 起点越界（服务器文件变小）：删 .part 从头下。
      await _drainBody(payload);
      if (!retried) {
        try {
          await File(partPath).delete();
        } catch (_) {
          // 删除失败也按从头下载继续。
        }
        await _attempt(
          job: job,
          targetPath: targetPath,
          partPath: partPath,
          resumeFrom: 0,
          token: token,
          retried: true,
        );
        return;
      }
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载失败，请重试',
        ),
      );
      return;
    }
    if (status == 200 && _isJsonContentType(contentType)) {
      // HTTP 200 + JSON = 信封错误（集数已删 1002、未登录 2001 等）。
      final body = await _readBody(payload);
      final failure = _envelopeFailure(body);
      _emit(key, failure);
      return;
    }
    if (status == 206 || status == 200) {
      var offset = resumeFrom;
      var total = 0;
      if (status == 206) {
        // totalBytes 取 Content-Range 总长，不是 Content-Length（剩余字节数）。
        total = parseContentRangeTotal(
              response.headers.value('content-range'),
            ) ??
            0;
      } else {
        if (resumeFrom > 0) {
          // 服务器不支持 Range（回 200 全量）：删 .part 从头下，
          // 当前 200 体即全量内容，直接从头写入，无需重发请求。
          try {
            await File(partPath).delete();
          } catch (_) {
            // 删除失败也按从头写入继续。
          }
          offset = 0;
        }
        total =
            int.tryParse(response.headers.value('content-length') ?? '') ?? 0;
      }
      await _streamToFile(
        job: job,
        body: payload,
        targetPath: targetPath,
        partPath: partPath,
        offset: offset,
        total: total,
        cancelToken: cancelToken,
      );
      return;
    }
    // 其余 <500 状态：JSON 按信封解析，否则通用失败。
    if (_isJsonContentType(contentType)) {
      final body = await _readBody(payload);
      _emit(key, _envelopeFailure(body));
      return;
    }
    await _drainBody(payload);
    _emit(
      key,
      const DownloadTaskState(
        status: DownloadStatus.failed,
        errorMessage: '下载失败，请重试',
      ),
    );
  }

  Future<void> _streamToFile({
    required _DownloadJob job,
    required ResponseBody body,
    required String targetPath,
    required String partPath,
    required int offset,
    required int total,
    required CancelToken cancelToken,
  }) async {
    final key = job.key;
    IOSink? sink;
    try {
      sink = File(
        partPath,
      ).openWrite(mode: offset > 0 ? FileMode.append : FileMode.write);
    } catch (_) {
      await _drainBody(body);
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载失败，请重试',
        ),
      );
      return;
    }

    var received = offset;
    var lastReportBytes = offset;
    var lastReport = DateTime.now();
    var userCancelled = false;
    var stallFired = false;
    StreamSubscription<Uint8List>? subscription;
    Timer? stallTimer;
    final done = Completer<void>();
    Object? streamError;

    void finishStall() {
      stallTimer?.cancel();
      stallTimer = null;
    }

    void resetStall() {
      finishStall();
      stallTimer = Timer(stallTimeout, () {
        stallFired = true;
        try {
          unawaited(subscription?.cancel());
        } catch (_) {
          // 取消订阅失败也按超时失败处理。
        }
        if (!done.isCompleted) {
          done.complete();
        }
      });
    }

    void report({bool force = false}) {
      final now = DateTime.now();
      if (force ||
          received - lastReportBytes >= _progressBytesThreshold ||
          now.difference(lastReport) >= _progressInterval) {
        lastReportBytes = received;
        lastReport = now;
        _emit(
          key,
          DownloadTaskState(
            status: DownloadStatus.downloading,
            receivedBytes: received,
            totalBytes: total,
          ),
        );
      }
    }

    // 用户取消：中断订阅，_attempt 的 Cancel 分支之外此处也兜底。
    unawaited(
      cancelToken.whenCancel.then((_) {
        userCancelled = true;
        try {
          unawaited(subscription?.cancel());
        } catch (_) {
          // 取消订阅失败也按取消处理。
        }
        if (!done.isCompleted) {
          done.complete();
        }
      }),
    );

    resetStall();
    try {
      subscription = body.stream.listen(
        (chunk) {
          if (stallFired || userCancelled || done.isCompleted) return;
          try {
            sink?.add(chunk);
          } catch (_) {
            // 写入失败在收尾校验中体现，此处仅累计避免进度倒退。
          }
          // received 累计包含 .part 已有偏移。
          received += chunk.length;
          resetStall();
          report();
        },
        onDone: () {
          if (!done.isCompleted) {
            done.complete();
          }
        },
        onError: (Object e) {
          streamError = e;
          if (!done.isCompleted) {
            done.complete();
          }
        },
        cancelOnError: false,
      );
      await done.future;
    } catch (_) {
      streamError ??= const FileSystemException('下载流异常');
    } finally {
      finishStall();
    }

    // 错误/取消路径必须 drain/关闭响应流与 IOSink，避免 dio 连接泄漏。
    if (userCancelled || cancelToken.isCancelled) {
      try {
        await subscription?.cancel();
      } catch (_) {
        // 订阅取消失败不阻塞清理。
      }
      try {
        await body.stream.drain<void>();
      } catch (_) {
        // drain 失败不阻塞清理。
      }
      try {
        await sink.flush();
      } catch (_) {
        // 刷盘失败不阻塞关闭。
      }
      try {
        await sink.close();
      } catch (_) {
        // 关闭失败不阻塞状态复位。
      }
      _emit(key, const DownloadTaskState(status: DownloadStatus.idle));
      return;
    }
    if (stallFired) {
      try {
        await subscription?.cancel();
      } catch (_) {
        // 订阅取消失败不阻塞清理。
      }
      try {
        await body.stream.drain<void>();
      } catch (_) {
        // drain 失败不阻塞清理。
      }
      try {
        await sink.flush();
      } catch (_) {
        // 刷盘失败不阻塞关闭。
      }
      try {
        await sink.close();
      } catch (_) {
        // 关闭失败不阻塞状态上报。
      }
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载超时，请重试',
        ),
      );
      return;
    }
    if (streamError != null) {
      try {
        await subscription?.cancel();
      } catch (_) {
        // 订阅取消失败不阻塞清理。
      }
      try {
        await sink.flush();
      } catch (_) {
        // 刷盘失败不阻塞关闭。
      }
      try {
        await sink.close();
      } catch (_) {
        // 关闭失败不阻塞状态上报。
      }
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载失败，请重试',
        ),
      );
      return;
    }

    report(force: true);
    if (total > 0 && received != total) {
      try {
        await sink.flush();
      } catch (_) {
        // 刷盘失败不阻塞关闭。
      }
      try {
        await sink.close();
      } catch (_) {
        // 关闭失败不阻塞状态上报。
      }
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载不完整',
        ),
      );
      return;
    }
    try {
      await sink.flush();
    } catch (_) {
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载失败，请重试',
        ),
      );
      try {
        await sink.close();
      } catch (_) {
        // 关闭失败不掩盖刷盘失败。
      }
      return;
    }
    try {
      await sink.close();
    } catch (_) {
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载失败，请重试',
        ),
      );
      return;
    }
    try {
      final target = File(targetPath);
      if (await target.exists()) {
        await target.delete();
      }
      await File(partPath).rename(targetPath);
    } catch (_) {
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载失败，请重试',
        ),
      );
      return;
    }
    final record = DownloadRecord(
      serverUrl: job.serverUrl,
      animeId: job.episode.animeId,
      episodeId: job.episode.id,
      epNumber: job.episode.epNumber,
      animeTitle: job.animeTitle,
      fileName: targetPath.split('/').last,
      fileSize: received,
      completedAt: DateTime.now().toIso8601String(),
    );
    try {
      await _store.upsert(record);
    } catch (_) {
      _emit(
        key,
        const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载失败，请重试',
        ),
      );
      return;
    }
    _completed[key] = record;
    _emit(
      key,
      DownloadTaskState(
        status: DownloadStatus.completed,
        receivedBytes: received,
        totalBytes: total > 0 ? total : received,
      ),
    );
  }

  DownloadTaskState _envelopeFailure(String body) {
    try {
      final decoded = jsonDecode(body);
      if (decoded is! Map) {
        return const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '下载失败，请重试',
        );
      }
      final map = Map<String, dynamic>.from(decoded);
      final code = (map['code'] as num?)?.toInt() ?? 9999;
      final message = map['message']?.toString() ?? '下载失败，请重试';
      if (code == 2001) {
        return const DownloadTaskState(
          status: DownloadStatus.failed,
          errorMessage: '登录状态已失效',
        );
      }
      return DownloadTaskState(
        status: DownloadStatus.failed,
        errorMessage: message,
      );
    } catch (_) {
      return const DownloadTaskState(
        status: DownloadStatus.failed,
        errorMessage: '下载失败，请重试',
      );
    }
  }

  Future<String> _readBody(ResponseBody body) async {
    try {
      final bytes = await body.stream.fold<List<int>>(
        <int>[],
        (previous, chunk) => previous..addAll(chunk),
      );
      return utf8.decode(bytes, allowMalformed: true);
    } catch (_) {
      return '';
    }
  }

  Future<void> _drainBody(ResponseBody body) async {
    try {
      await body.stream.drain<void>();
    } catch (_) {
      // drain 失败不阻塞后续处理。
    }
  }

  Future<String> _resolveRoot() async {
    final root = await _ref.read(downloadsRootProvider.future);
    _cachedRoot = root;
    return root;
  }

  void _emit(DownloadKey key, DownloadTaskState state) {
    if (_disposed) return;
    try {
      onStateChanged?.call(key, state);
    } catch (_) {
      // 回调异常不中断下载流程。
    }
  }
}
