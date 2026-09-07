import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/api/media_api.dart';
import 'package:fan_web/models/anime.dart';
import 'package:fan_web/models/download.dart';
import 'package:fan_web/models/user.dart';
import 'package:fan_web/providers/auth_provider.dart';
import 'package:fan_web/providers/download_provider.dart';
import 'package:fan_web/services/episode_downloader.dart';

const _serverUrl = 'http://192.168.1.10:8080';
const _animeTitle = '超辉夜姬！';

const _authedState = AuthState.authenticated(
  user: User(id: 1, username: 'tester', isAdmin: false, createdAt: ''),
  token: 'jwt-token',
  serverUrl: _serverUrl,
);

/// 测试文件内自带的流式 stub（不改公共 http_stub.dart）。
class _StreamStubAdapter implements HttpClientAdapter {
  _StreamStubAdapter(this.handler);

  final ResponseBody Function(RequestOptions options) handler;
  final List<RequestOptions> requests = [];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options);
    return handler(options);
  }

  @override
  void close({bool force = false}) {}
}

class _FakeAuthNotifier extends AuthNotifier {
  _FakeAuthNotifier(this.fixed);

  final AuthState fixed;

  @override
  AuthState build() => fixed;
}

final _probeProvider = Provider<Ref>((ref) => ref);

Episode _episode({
  int id = 23,
  int animeId = 9,
  int epNumber = 1,
  String filePath = '9_23.mp4',
}) {
  return Episode(
    id: id,
    animeId: animeId,
    epNumber: epNumber,
    title: 'test',
    filePath: filePath,
    duration: 0,
  );
}

ResponseBody _bytesBody(
  List<int> bytes, {
  required int status,
  Map<String, List<String>>? headers,
}) {
  return ResponseBody(
    Stream<Uint8List>.fromIterable([Uint8List.fromList(bytes)]),
    status,
    headers: headers,
  );
}

ResponseBody _jsonEnvelope(int code, String message, {int status = 200}) {
  return ResponseBody.fromString(
    jsonEncode({'code': code, 'message': message, 'data': null}),
    status,
    headers: {
      Headers.contentTypeHeader: ['application/json'],
    },
  );
}

String _expectedDir(String root, String serverUrl) {
  final digest = sha256.convert(utf8.encode(serverUrl)).toString();
  return '$root/downloads/${digest.substring(0, 16)}';
}

class _Harness {
  _Harness({
    required this.container,
    required this.downloader,
    required this.adapter,
    required this.states,
    required this.tempDir,
  });

  final ProviderContainer container;
  final EpisodeDownloader downloader;
  final _StreamStubAdapter adapter;
  final List<MapEntry<DownloadKey, DownloadTaskState>> states;
  final Directory tempDir;

  DownloadTaskState? stateFor(int episodeId) {
    for (var i = states.length - 1; i >= 0; i--) {
      final entry = states[i];
      if (entry.key.episodeId == episodeId) {
        return entry.value;
      }
    }
    return null;
  }

  Future<void> dispose() async {
    downloader.dispose();
    container.dispose();
    try {
      await tempDir.delete(recursive: true);
    } catch (_) {
      // 临时目录清理失败不影响断言。
    }
  }
}

Future<_Harness> _setUp({
  required ResponseBody Function(RequestOptions options) handler,
  AuthState authState = _authedState,
  Duration stallTimeout = const Duration(seconds: 60),
}) async {
  final tempDir = await Directory.systemTemp.createTemp('dl_test_');
  final adapter = _StreamStubAdapter(handler);
  final dio = Dio()..httpClientAdapter = adapter;
  final container = ProviderContainer(
    overrides: [
      downloadsRootProvider.overrideWith((ref) async => tempDir.path),
      authProvider.overrideWith(() => _FakeAuthNotifier(authState)),
    ],
  );
  final ref = container.read(_probeProvider);
  final states = <MapEntry<DownloadKey, DownloadTaskState>>[];
  final downloader = EpisodeDownloader(
    ref,
    dio: dio,
    stallTimeout: stallTimeout,
    onStateChanged: (key, state) {
      states.add(MapEntry(key, state));
    },
  );
  return _Harness(
    container: container,
    downloader: downloader,
    adapter: adapter,
    states: states,
    tempDir: tempDir,
  );
}

void main() {
  group('buildDownloadUrl', () {
    test('builds download path without any token', () {
      expect(
        buildDownloadUrl('http://192.168.1.10:8080///', 23),
        'http://192.168.1.10:8080/api/episodes/23/download',
      );
    });
  });

  group('DownloadIndexCodec', () {
    test('round-trips records', () {
      const record = DownloadRecord(
        serverUrl: _serverUrl,
        animeId: 9,
        episodeId: 23,
        epNumber: 1,
        animeTitle: _animeTitle,
        fileName: '9_23.mp4',
        fileSize: 10,
        completedAt: '2026-09-06T21:00:00.000',
      );
      final decoded = DownloadIndexCodec.decode(
        DownloadIndexCodec.encode([record]),
      );
      expect(decoded, hasLength(1));
      expect(decoded.single.fileName, '9_23.mp4');
      expect(decoded.single.fileSize, 10);
    });

    test('damaged input returns empty list', () {
      expect(DownloadIndexCodec.decode('not-json'), isEmpty);
      expect(DownloadIndexCodec.decode(''), isEmpty);
      expect(DownloadIndexCodec.decode('{"version":1}'), isEmpty);
    });
  });

  group('EpisodeDownloader', () {
    test('206 resume accumulates the existing .part offset', () async {
      final remaining = [5, 6, 7, 8, 9, 10];
      final harness = await _setUp(
        handler: (options) => _bytesBody(
          remaining,
          status: 206,
          headers: {
            'content-range': ['bytes 4-9/10'],
            'content-type': ['application/octet-stream'],
            'content-length': ['6'],
          },
        ),
      );
      try {
        final dir = _expectedDir(harness.tempDir.path, _serverUrl);
        await Directory(dir).create(recursive: true);
        await File('$dir/9_23.mp4.part').writeAsBytes([1, 2, 3, 4]);

        await harness.downloader.enqueue(
          serverUrl: _serverUrl,
          episode: _episode(),
          animeTitle: _animeTitle,
        );

        expect(
          harness.adapter.requests.single.headers['Range'],
          'bytes=4-',
        );
        final path = harness.downloader.localFilePath(_serverUrl, 23);
        expect(path, isNotNull);
        expect(await File(path!).readAsBytes(), [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
        final last = harness.stateFor(23);
        expect(last?.status, DownloadStatus.completed);
        // received 含 .part 偏移：完成态累计为全量 10 字节。
        expect(last?.receivedBytes, 10);
        expect(last?.totalBytes, 10);
      } finally {
        await harness.dispose();
      }
    });

    test('200 JSON envelope surfaces the business message', () async {
      final harness = await _setUp(
        handler: (options) => _jsonEnvelope(1002, '集数不存在'),
      );
      try {
        final dir = _expectedDir(harness.tempDir.path, _serverUrl);
        await Directory(dir).create(recursive: true);
        final part = File('$dir/9_23.mp4.part');
        await part.writeAsBytes([1, 2, 3]);

        await harness.downloader.enqueue(
          serverUrl: _serverUrl,
          episode: _episode(),
          animeTitle: _animeTitle,
        );

        final last = harness.stateFor(23);
        expect(last?.status, DownloadStatus.failed);
        expect(last?.errorMessage, '集数不存在');
        // 信封错误保留 .part。
        expect(await part.exists(), isTrue);
        expect(harness.downloader.localFilePath(_serverUrl, 23), isNull);
      } finally {
        await harness.dispose();
      }
    });

    test('404 maps to the unsupported message', () async {
      final harness = await _setUp(
        handler: (options) => ResponseBody.fromString(
          'not found',
          404,
          headers: {
            Headers.contentTypeHeader: ['text/plain'],
          },
        ),
      );
      try {
        await harness.downloader.enqueue(
          serverUrl: _serverUrl,
          episode: _episode(),
          animeTitle: _animeTitle,
        );

        final last = harness.stateFor(23);
        expect(last?.status, DownloadStatus.failed);
        expect(last?.errorMessage, '服务器不支持下载或文件不存在');
      } finally {
        await harness.dispose();
      }
    });

    test('stall beyond the injected timeout fails', () async {
      final controller = StreamController<Uint8List>();
      controller.add(Uint8List.fromList([1, 2, 3, 4]));
      // 此后不再推送也不关闭，模拟连接卡死。
      final harness = await _setUp(
        stallTimeout: const Duration(milliseconds: 200),
        handler: (options) => ResponseBody(
          controller.stream,
          206,
          headers: {
            'content-range': ['bytes 0-99/100'],
            'content-type': ['application/octet-stream'],
          },
        ),
      );
      try {
        await harness.downloader.enqueue(
          serverUrl: _serverUrl,
          episode: _episode(),
          animeTitle: _animeTitle,
        );

        final last = harness.stateFor(23);
        expect(last?.status, DownloadStatus.failed);
        expect(last?.errorMessage, contains('超时'));
        // 卡死失败保留 .part（含已收的首块）。
        final part = File(
          '${_expectedDir(harness.tempDir.path, _serverUrl)}/9_23.mp4.part',
        );
        expect(await part.exists(), isTrue);
        expect(await part.length(), 4);
      } finally {
        await harness.dispose();
        try {
          await controller.close();
        } catch (_) {
          // 控制器关闭失败不影响断言。
        }
      }
    });

    test('size mismatch fails with incomplete error', () async {
      final harness = await _setUp(
        handler: (options) => _bytesBody(
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
          status: 206,
          headers: {
            'content-range': ['bytes 0-9/100'],
            'content-type': ['application/octet-stream'],
            'content-length': ['10'],
          },
        ),
      );
      try {
        await harness.downloader.enqueue(
          serverUrl: _serverUrl,
          episode: _episode(),
          animeTitle: _animeTitle,
        );

        final last = harness.stateFor(23);
        expect(last?.status, DownloadStatus.failed);
        expect(last?.errorMessage, '下载不完整');
        final dir = _expectedDir(harness.tempDir.path, _serverUrl);
        expect(await File('$dir/9_23.mp4').exists(), isFalse);
        expect(await File('$dir/9_23.mp4.part').exists(), isTrue);
      } finally {
        await harness.dispose();
      }
    });

    test('empty token fails with login message without requesting', () async {
      final harness = await _setUp(
        authState: const AuthState.unauthenticated(serverUrl: _serverUrl),
        handler: (options) => _bytesBody([1], status: 200),
      );
      try {
        await harness.downloader.enqueue(
          serverUrl: _serverUrl,
          episode: _episode(),
          animeTitle: _animeTitle,
        );

        final last = harness.stateFor(23);
        expect(last?.status, DownloadStatus.failed);
        expect(last?.errorMessage, '请先登录');
        expect(harness.adapter.requests, isEmpty);
      } finally {
        await harness.dispose();
      }
    });

    test('completed download persists through restoreIndex', () async {
      final bytes = [for (var i = 0; i < 8; i++) i];
      final harness = await _setUp(
        handler: (options) => _bytesBody(
          bytes,
          status: 200,
          headers: {
            'content-type': ['application/octet-stream'],
            'content-length': ['8'],
          },
        ),
      );
      try {
        await harness.downloader.enqueue(
          serverUrl: _serverUrl,
          episode: _episode(filePath: 'dir/clip.mkv'),
          animeTitle: _animeTitle,
        );
        expect(harness.stateFor(24 - 1)?.status, DownloadStatus.completed);

        final indexFile = File(
          '${harness.tempDir.path}/downloads/index.json',
        );
        expect(await indexFile.exists(), isTrue);

        // 用同一 ref 新建引擎模拟重启后恢复。
        final ref = harness.container.read(_probeProvider);
        final restoredStates = <MapEntry<DownloadKey, DownloadTaskState>>[];
        final second = EpisodeDownloader(
          ref,
          dio: Dio()..httpClientAdapter = _StreamStubAdapter(
            (options) => _bytesBody([9], status: 200),
          ),
          onStateChanged: (key, state) {
            restoredStates.add(MapEntry(key, state));
          },
        );
        try {
          await second.restoreIndex();
          expect(
            second.localFilePath(_serverUrl, 23),
            harness.downloader.localFilePath(_serverUrl, 23),
          );
          expect(second.completedRecords, hasLength(1));
          expect(second.completedRecords.single.fileName, '9_23.mkv');
        } finally {
          second.dispose();
        }
      } finally {
        await harness.dispose();
      }
    });
  });
}
