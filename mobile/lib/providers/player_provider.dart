import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';
import 'package:screen_brightness/screen_brightness.dart';
import 'package:volume_controller/volume_controller.dart';

import '../api/media_api.dart';
import '../api/progress_api.dart';
import '../models/anime.dart';
import '../services/progress_outbox.dart';
import 'anime_provider.dart';
import 'auth_provider.dart';

@immutable
class PlayerLaunchInfo {
  const PlayerLaunchInfo({this.animeTitle, this.episodeNumber});

  final String? animeTitle;
  final int? episodeNumber;
}

@immutable
class PlayerConfig {
  const PlayerConfig({
    required this.animeId,
    required this.episodeId,
    required this.serverUrl,
    required this.token,
    this.animeTitle,
    this.episodeNumber,
  });

  final int animeId;
  final int episodeId;
  final String serverUrl;
  final String token;
  final String? animeTitle;
  final int? episodeNumber;

  @override
  bool operator ==(Object other) {
    return other is PlayerConfig &&
        other.animeId == animeId &&
        other.episodeId == episodeId &&
        other.serverUrl == serverUrl &&
        other.token == token &&
        other.animeTitle == animeTitle &&
        other.episodeNumber == episodeNumber;
  }

  @override
  int get hashCode => Object.hash(
    animeId,
    episodeId,
    serverUrl,
    token,
    animeTitle,
    episodeNumber,
  );
}

@immutable
class PlayerState {
  const PlayerState({
    this.isInitialized = false,
    this.isLoading = false,
    this.isPlaying = false,
    this.isBuffering = false,
    this.position = Duration.zero,
    this.duration = Duration.zero,
    this.error,
    this.isCompleted = false,
    this.subtitleTracks = const [],
    this.currentSubtitleTrack,
    this.isRestoring = false,
    this.savedPosition = 0,
    this.restoredPosition,
    this.volume = 1,
    this.brightness = 0.5,
    this.subtitleFontSize = 20,
    this.playbackRate = 1,
    this.notice,
  });

  final bool isInitialized;
  final bool isLoading;
  final bool isPlaying;
  final bool isBuffering;
  final Duration position;
  final Duration duration;
  final String? error;
  final bool isCompleted;
  final List<SubtitleTrack> subtitleTracks;
  final SubtitleTrack? currentSubtitleTrack;
  final bool isRestoring;
  final int savedPosition;
  final int? restoredPosition;
  final double volume;
  final double brightness;
  final double subtitleFontSize;
  final double playbackRate;
  final String? notice;

  PlayerState copyWith({
    bool? isInitialized,
    bool? isLoading,
    bool? isPlaying,
    bool? isBuffering,
    Duration? position,
    Duration? duration,
    String? error,
    bool clearError = false,
    bool? isCompleted,
    List<SubtitleTrack>? subtitleTracks,
    SubtitleTrack? currentSubtitleTrack,
    bool clearCurrentSubtitleTrack = false,
    bool? isRestoring,
    int? savedPosition,
    int? restoredPosition,
    bool clearRestoredPosition = false,
    double? volume,
    double? brightness,
    double? subtitleFontSize,
    double? playbackRate,
    String? notice,
    bool clearNotice = false,
  }) {
    return PlayerState(
      isInitialized: isInitialized ?? this.isInitialized,
      isLoading: isLoading ?? this.isLoading,
      isPlaying: isPlaying ?? this.isPlaying,
      isBuffering: isBuffering ?? this.isBuffering,
      position: position ?? this.position,
      duration: duration ?? this.duration,
      error: error ?? (clearError ? null : this.error),
      isCompleted: isCompleted ?? this.isCompleted,
      subtitleTracks: subtitleTracks ?? this.subtitleTracks,
      currentSubtitleTrack:
          currentSubtitleTrack ??
          (clearCurrentSubtitleTrack ? null : this.currentSubtitleTrack),
      isRestoring: isRestoring ?? this.isRestoring,
      savedPosition: savedPosition ?? this.savedPosition,
      restoredPosition:
          restoredPosition ??
          (clearRestoredPosition ? null : this.restoredPosition),
      volume: volume ?? this.volume,
      brightness: brightness ?? this.brightness,
      subtitleFontSize: subtitleFontSize ?? this.subtitleFontSize,
      playbackRate: playbackRate ?? this.playbackRate,
      notice: notice ?? (clearNotice ? null : this.notice),
    );
  }
}

final playerProvider = NotifierProvider.autoDispose
    .family<PlayerNotifier, PlayerState, PlayerConfig>(PlayerNotifier.new);

class PlayerNotifier extends Notifier<PlayerState> {
  PlayerNotifier(this.config);

  final PlayerConfig config;

  late final Player player;
  late final VideoController videoController;

  final _subscriptions = <StreamSubscription<dynamic>>[];
  Timer? _progressTimer;
  Future<void>? _reportChain;
  Future<void>? _disposeFuture;

  late final ProgressApi _progressApi;
  int _savedPosition = 0;
  bool _restoreHandled = false;
  bool _openCompleted = false;
  bool _startedPlayback = false;
  bool _hasOpened = false;
  bool _brightnessChanged = false;
  bool _defaultSubtitleApplied = false;
  bool _subtitleUserSelected = false;
  bool _disposing = false;
  bool _disposed = false;
  DateTime? _mediaExpiresAt;
  Timer? _mediaRefreshTimer;
  bool _mediaTokenRefreshInFlight = false;
  bool _didNearExpiryReopen = false;

  @override
  PlayerState build() {
    _progressApi = ref.read(progressApiProvider);
    player = Player();
    videoController = VideoController(player);
    _setupListeners();
    ref.onDispose(() {
      unawaited(dispose());
    });
    unawaited(_initialize());
    unawaited(_loadBrightness());
    unawaited(_loadVolume());
    return PlayerState(isInitialized: true, isLoading: true);
  }

  PlayerState get currentState => state;

  void _setupListeners() {
    _subscriptions.add(player.stream.playing.listen(_handlePlaying));
    _subscriptions.add(player.stream.completed.listen(_handleCompleted));
    _subscriptions.add(player.stream.position.listen(_handlePosition));
    _subscriptions.add(player.stream.duration.listen(_handleDuration));
    _subscriptions.add(player.stream.buffering.listen(_handleBuffering));
    _subscriptions.add(player.stream.tracks.listen(_handleTracks));
    _subscriptions.add(player.stream.track.listen(_handleTrack));
    _subscriptions.add(player.stream.error.listen(_handleError));
  }

  Future<void> _initialize() async {
    // 先同步 outbox 中待上传的进度，确保服务端有最新位置后再查询断点
    try {
      final authState = ref.read(authProvider);
      final serverUrl = authState.serverUrl ?? config.serverUrl;
      final userId = authState.user?.id ?? 0;
      final token = authState.token ?? config.token;
      if (userId > 0 && token.isNotEmpty) {
        final outbox = ref.read(progressOutboxProvider);
        await outbox.syncAll(serverUrl, userId, token);
      }
    } catch (_) {}
    if (_disposed || _disposing) {
      return;
    }

    try {
      final progress = await _progressApi.getEpisodeProgress(config.episodeId);
      if (_disposed || _disposing) {
        return;
      }
      _savedPosition = progress.position < 0 ? 0 : progress.position;
      _setState(
        state.copyWith(
          savedPosition: _savedPosition,
          isRestoring: _savedPosition > 0,
        ),
      );
    } catch (_) {
      // 进度查询失败时仍尝试打开视频，避免一次接口异常阻塞播放。
    }

    if (_disposed || _disposing) {
      return;
    }

    try {
      final Media media = await _buildOpenMedia();
      if (_disposed || _disposing) {
        return;
      }
      await player.open(media, play: false);
      if (_disposed || _disposing) {
        return;
      }
      _hasOpened = true;
      _openCompleted = true;
      _startProgressTimer();
      final duration = await _waitForDuration();
      await _restoreAndStart(duration);
    } on MediaTokenUnsupported {
      _setState(state.copyWith(isLoading: false, error: '当前服务器不支持媒体票据，请升级服务器'));
      return;
    } catch (_) {
      if (_isWithin60sOfExpiry() && !_didNearExpiryReopen) {
        _didNearExpiryReopen = true;
        try {
          await _refreshMediaToken();
          if (_disposed || _disposing) {
            return;
          }
          _hasOpened = true;
          _openCompleted = true;
          _startProgressTimer();
          final duration = await _waitForDuration();
          await _restoreAndStart(duration);
          return;
        } on MediaTokenUnsupported {
          _setState(
            state.copyWith(isLoading: false, error: '当前服务器不支持媒体票据，请升级服务器'),
          );
          return;
        } catch (_) {}
      }
      _setState(state.copyWith(isLoading: false, error: '播放失败，请检查网络或重新登录'));
      return;
    }
  }

  // 使用媒体票据打开播放地址；票据接口不可用时按硬失败处理。
  Future<Media> _buildOpenMedia() async {
    final authState = ref.read(authProvider);
    final serverUrl = authState.serverUrl ?? config.serverUrl;
    final mediaApi = ref.read(mediaApiProvider);
    final built = await buildPlayerMedia(
      requestMediaToken: mediaApi.fetchMediaToken,
      serverUrl: serverUrl,
      episodeId: config.episodeId,
      startPositionSeconds: _savedPosition,
    );
    _mediaExpiresAt = built.expiresAt;
    _scheduleMediaTokenRefresh(built.expiresAt);
    return built.media;
  }

  void _scheduleMediaTokenRefresh(DateTime? expiresAt) {
    _mediaRefreshTimer?.cancel();
    if (expiresAt == null || !expiresAt.isAfter(DateTime.now())) {
      return;
    }
    var delay =
        expiresAt.difference(DateTime.now()) - const Duration(minutes: 5);
    if (delay < const Duration(minutes: 1)) {
      delay = const Duration(minutes: 1);
    }
    _mediaRefreshTimer = Timer(delay, () {
      unawaited(_refreshMediaToken());
    });
  }

  bool _isWithin60sOfExpiry() {
    final expiresAt = _mediaExpiresAt;
    if (expiresAt == null) {
      return false;
    }
    return !DateTime.now().isBefore(
      expiresAt.subtract(const Duration(seconds: 60)),
    );
  }

  Future<void> _refreshMediaToken() async {
    if (_mediaTokenRefreshInFlight || _disposed || _disposing) {
      return;
    }
    _mediaTokenRefreshInFlight = true;
    try {
      final media = await _buildOpenMedia();
      if (_disposed || _disposing) {
        return;
      }
      final resume = state.position;
      final shouldPlay = state.isPlaying;
      await player.open(media, play: false);
      _hasOpened = true;
      _didNearExpiryReopen = false;
      if (resume > Duration.zero) {
        try {
          await player.seek(resume);
        } catch (_) {}
      }
      if (shouldPlay) {
        try {
          await player.play();
        } catch (_) {}
      }
    } finally {
      _mediaTokenRefreshInFlight = false;
    }
  }

  void _handlePlaying(bool playing) {
    final wasPlaying = state.isPlaying;
    _setState(state.copyWith(isPlaying: playing));
    if (!playing &&
        wasPlaying &&
        !state.isRestoring &&
        !_disposing &&
        !_disposed) {
      unawaited(_queueCurrentProgress());
    }
  }

  void _handleCompleted(bool completed) {
    _setState(
      state.copyWith(
        isCompleted: completed,
        isPlaying: completed ? false : state.isPlaying,
      ),
    );
    if (completed && !_disposing && !_disposed) {
      unawaited(_queueCurrentProgress(forceWatched: true));
    }
  }

  void _handlePosition(Duration position) {
    _setState(state.copyWith(position: position));
  }

  void _handleDuration(Duration duration) {
    _setState(state.copyWith(duration: duration));
  }

  void _handleBuffering(bool buffering) {
    _setState(state.copyWith(isBuffering: buffering));
  }

  void _handleTracks(Tracks tracks) {
    final subtitleTracks = tracks.subtitle
        .where((track) => track.id != 'auto' && track.id != 'no')
        .toList(growable: false);
    _setState(
      state.copyWith(subtitleTracks: List.unmodifiable(subtitleTracks)),
    );
    if (!_defaultSubtitleApplied &&
        !_subtitleUserSelected &&
        subtitleTracks.isNotEmpty) {
      unawaited(_applyDefaultSubtitle(subtitleTracks.first));
    }
  }

  Future<void> _applyDefaultSubtitle(SubtitleTrack track) async {
    _defaultSubtitleApplied = true;
    try {
      await player.setSubtitleTrack(track);
      if (!_disposed && !_disposing) {
        _setState(state.copyWith(currentSubtitleTrack: track));
      }
    } catch (_) {
      _defaultSubtitleApplied = false;
    }
  }

  void _handleTrack(Track track) {
    final subtitle = track.subtitle;
    if (subtitle.id == 'auto' || subtitle.id == 'no') {
      _setState(state.copyWith(clearCurrentSubtitleTrack: true));
      return;
    }
    _setState(state.copyWith(currentSubtitleTrack: subtitle));
  }

  void _handleError(String message) {
    if (_mediaTokenRefreshInFlight) {
      return;
    }
    if (_isWithin60sOfExpiry()) {
      if (!_didNearExpiryReopen) {
        _didNearExpiryReopen = true;
        unawaited(_refreshMediaToken());
      }
      return;
    }
    _setState(
      state.copyWith(
        isLoading: false,
        error: message.trim().isEmpty ? '播放失败，请检查网络或重新登录' : message,
      ),
    );
  }

  Future<Duration> _waitForDuration() async {
    final current = player.state.duration;
    if (current > Duration.zero) {
      return current;
    }
    try {
      return await player.stream.duration
          .firstWhere((duration) => duration > Duration.zero)
          .timeout(const Duration(seconds: 8));
    } on TimeoutException {
      return player.state.duration;
    }
  }

  Future<void> _restoreAndStart(Duration duration) async {
    if (_disposed || _disposing || !_openCompleted || _restoreHandled) {
      return;
    }

    if (_savedPosition <= 0) {
      _restoreHandled = true;
      _setState(state.copyWith(isLoading: false, isRestoring: false));
      await _startPlaybackIfReady();
      return;
    }

    if (duration > Duration.zero && _savedPosition >= duration.inSeconds) {
      try {
        await player.seek(Duration.zero);
      } catch (_) {
        // 已看完或服务端记录越界时，仍允许从头播放。
      }
      _restoreHandled = true;
      _setState(
        state.copyWith(
          isLoading: false,
          isRestoring: false,
          position: Duration.zero,
        ),
      );
      await _startPlaybackIfReady();
      return;
    }

    final target = Duration(seconds: _savedPosition);
    final restored = await _confirmResumePosition(target);
    if (_disposed || _disposing) {
      return;
    }
    if (!restored) {
      // 断点恢复失败时不进入终态 error（媒体已加载、从头播放完全可行），
      // 回退为从头播放，但保留原来的正进度哨兵，避免后续 0 上报覆盖服务端断点。
      final origSaved = _savedPosition;
      _restoreHandled = true;
      _setState(
        state.copyWith(
          isLoading: false,
          isRestoring: false,
          savedPosition: origSaved,
          position: Duration.zero,
        ),
      );
      try {
        await player.seek(Duration.zero);
      } catch (_) {
        // seek 失败也允许继续播放（不支持 seek 的流从当前位置开始）。
      }
      await _startPlaybackIfReady();
      return;
    }

    _restoreHandled = true;
    _setState(
      state.copyWith(
        position: player.state.position,
        isLoading: false,
        isRestoring: false,
        restoredPosition: _savedPosition,
      ),
    );
    await _startPlaybackIfReady();
  }

  Future<bool> _confirmResumePosition(Duration target) async {
    if (_isPositionNear(player.state.position, target)) {
      return true;
    }
    // Media.start 在 libmpv 的 on_load 钩子中生效，先等待原生层完成起播定位。
    if (await _waitForPosition(target, const Duration(seconds: 4))) {
      return true;
    }

    // 极慢网络下 on_load 可能延迟；文件已加载后再用 seek 做有限兜底。
    for (var attempt = 0; attempt < 3; attempt++) {
      if (_disposed || _disposing) {
        return false;
      }
      if (_isPositionNear(player.state.position, target)) {
        return true;
      }
      try {
        await player.seek(target);
      } catch (_) {
        // 下一轮继续尝试，避免一次 seek 请求失败导致从 0 播放。
      }
      if (_isPositionNear(player.state.position, target)) {
        return true;
      }
      if (await _waitForPosition(target, const Duration(milliseconds: 1200))) {
        return true;
      }
    }
    return false;
  }

  Future<bool> _waitForPosition(Duration target, Duration timeout) async {
    final completer = Completer<bool>();
    late final StreamSubscription<Duration> subscription;
    Timer? timer;

    void complete(bool value) {
      if (!completer.isCompleted) {
        completer.complete(value);
      }
    }

    subscription = player.stream.position.listen((position) {
      if (_isPositionNear(position, target)) {
        complete(true);
      }
    });
    timer = Timer(timeout, () => complete(false));

    final result = await completer.future;
    timer.cancel();
    await subscription.cancel();
    return result;
  }

  Future<void> _startPlaybackIfReady() async {
    if (!_openCompleted ||
        _startedPlayback ||
        !_restoreHandled ||
        state.isRestoring ||
        _disposed ||
        _disposing) {
      return;
    }
    _startedPlayback = true;
    try {
      await player.play();
    } catch (_) {
      _startedPlayback = false;
      _setState(state.copyWith(error: '播放失败，请检查网络或重新登录'));
    }
  }

  void _startProgressTimer() {
    _progressTimer?.cancel();
    _progressTimer = Timer.periodic(const Duration(seconds: 15), (_) {
      if (state.isPlaying &&
          !state.isBuffering &&
          !state.isRestoring &&
          !_disposing &&
          !_disposed) {
        unawaited(_queueCurrentProgress());
      }
    });
  }

  Future<void> play() async {
    if (_disposed || _disposing) {
      return;
    }
    await player.play();
  }

  Future<void> pause() async {
    if (_disposed || _disposing) {
      return;
    }
    try {
      await player.pause();
    } finally {
      await _queueCurrentProgress();
    }
  }

  Future<void> pauseAndReport() => pause();

  Future<void> seek(Duration position) async {
    if (_disposed || _disposing) {
      return;
    }
    final target = _clampDuration(position, state.duration);
    try {
      await player.seek(target);
      _setState(state.copyWith(position: target, isCompleted: false));
    } catch (_) {
      _setState(state.copyWith(notice: '跳转失败，请稍后重试'));
    }
  }

  Future<void> setVolume(double value) async {
    if (_disposed || _disposing) {
      return;
    }
    final normalized = _clamp01(value);
    try {
      await VolumeController.instance.setVolume(normalized);
    } catch (_) {
      // 某些平台不支持设置音量时保持播放不中断。
    }
    _setState(state.copyWith(volume: normalized));
  }

  Future<void> setBrightness(double value) async {
    if (_disposed || _disposing) {
      return;
    }
    final normalized = _clamp01(value);
    try {
      await ScreenBrightness.instance.setApplicationScreenBrightness(
        normalized,
      );
      _brightnessChanged = true;
    } catch (_) {
      // 某些平台不支持设置亮度时保持播放不中断。
    }
    _setState(state.copyWith(brightness: normalized));
  }

  Future<void> setSubtitleTrack(SubtitleTrack track) async {
    if (_disposed || _disposing) {
      return;
    }
    _subtitleUserSelected = true;
    try {
      await player.setSubtitleTrack(track);
      if (track.id == 'no' || track.id == 'auto') {
        _setState(state.copyWith(clearCurrentSubtitleTrack: true));
      } else {
        _setState(state.copyWith(currentSubtitleTrack: track));
      }
    } catch (_) {
      _setState(state.copyWith(notice: '字幕切换失败'));
    }
  }

  Future<void> setNoSubtitle() => setSubtitleTrack(SubtitleTrack.no());

  void setSubtitleFontSize(double value) {
    if (_disposed || _disposing) {
      return;
    }
    _setState(state.copyWith(subtitleFontSize: value.clamp(20, 28).toDouble()));
  }

  Future<void> setPlaybackRate(double value) async {
    if (_disposed || _disposing) {
      return;
    }
    final rate = normalizePlaybackRate(value);
    try {
      await player.setRate(rate);
      if (!_disposed && !_disposing) {
        _setState(state.copyWith(playbackRate: rate));
      }
    } catch (_) {
      _setState(state.copyWith(notice: '倍速切换失败'));
    }
  }

  void consumeRestoredPosition() {
    if (_disposed || state.restoredPosition == null) {
      return;
    }
    _setState(state.copyWith(clearRestoredPosition: true));
  }

  void consumeNotice() {
    if (_disposed || state.notice == null) {
      return;
    }
    _setState(state.copyWith(clearNotice: true));
  }

  Future<void> _loadBrightness() async {
    try {
      final value = await ScreenBrightness.instance.system;
      if (!_disposed && !_disposing) {
        _setState(state.copyWith(brightness: _clamp01(value)));
      }
    } catch (_) {
      // 读取失败时使用默认亮度，不影响播放器。
    }
  }

  Future<void> _loadVolume() async {
    try {
      final value = await VolumeController.instance.getVolume();
      if (!_disposed && !_disposing) {
        _setState(state.copyWith(volume: _clamp01(value)));
      }
    } catch (_) {
      // 读取失败时使用默认音量，不影响播放器。
    }
  }

  Future<void> _queueCurrentProgress({bool forceWatched = false}) async {
    await _queueProgressReport(
      position: state.position,
      watched: forceWatched || _isWatched(state.position),
    );
  }

  Future<void> _queueProgressReport({
    required Duration position,
    required bool watched,
  }) async {
    if (_disposed && !_disposing) {
      return;
    }
    if (!_hasOpened && !_disposing) {
      return;
    }
    // 断点恢复确认完成前（媒体尚未定位到断点，position 为 0 或瞬态值）
    // 不上报也不写 outbox，避免覆盖服务端已保存的断点；与 _handlePlaying/定时器路径一致。
    if (state.isRestoring) {
      return;
    }
    final safePosition = position.inSeconds < 0 ? 0 : position.inSeconds;
    if (!shouldQueueProgress(safePosition)) {
      return;
    }
    final now = DateTime.now().toIso8601String();
    final authState = ref.read(authProvider);
    final serverUrl = authState.serverUrl ?? config.serverUrl;
    final userId = authState.user?.id ?? 0;
    final outbox = ref.read(progressOutboxProvider);
    // 立即持久化到 outbox，防止进程在网络请求期间被杀死导致进度丢失
    final record = PendingProgress(
      serverUrl: serverUrl,
      userId: userId,
      episodeId: config.episodeId,
      position: safePosition,
      watched: watched,
      updatedAt: now,
    );
    await outbox.save(record);
    if (_disposed || _disposing) {
      return Future<void>.value();
    }
    // 网络发送放入串行队列，失败时 outbox 中的记录保留
    final previous = _reportChain ?? Future<void>.value();
    final next = previous.then<void>((_) async {
      try {
        await _progressApi.reportProgress(
          config.episodeId,
          safePosition,
          watched,
        );
        await outbox.removeIfMatched(record);
      } catch (_) {
        // 上报失败不打断播放或后续上报，outbox 中的记录会在下次重试。
      }
    });
    _reportChain = next;
    return next;
  }

  bool _isWatched(Duration position) {
    final totalMilliseconds = state.duration.inMilliseconds;
    return totalMilliseconds > 0 &&
        position.inMilliseconds >= totalMilliseconds * 0.9;
  }

  Future<void> dispose() {
    final running = _disposeFuture;
    if (running != null) {
      return running;
    }
    final future = _disposeInternal();
    _disposeFuture = future;
    return future;
  }

  Future<void> _disposeInternal() async {
    if (_disposed) {
      return;
    }
    _disposing = true;
    _disposed = true;
    _progressTimer?.cancel();
    _progressTimer = null;
    _mediaRefreshTimer?.cancel();
    _mediaRefreshTimer = null;
    for (final subscription in _subscriptions) {
      await subscription.cancel();
    }
    _subscriptions.clear();

    if (_hasOpened && shouldQueueProgress(state.position.inSeconds)) {
      await _queueCurrentProgress();
    }
    final reportChain = _reportChain;
    if (reportChain != null) {
      await reportChain;
    }

    try {
      await player.dispose();
    } catch (_) {
      // 释放失败也不能阻塞页面退出。
    }
    if (_brightnessChanged) {
      try {
        await ScreenBrightness.instance.resetApplicationScreenBrightness();
      } catch (_) {
        // 恢复系统亮度失败时交由系统生命周期处理。
      }
    }
    _disposing = false;
  }

  void _setState(PlayerState next) {
    if (!_disposed) {
      state = next;
    }
  }
}

bool _isPositionNear(Duration actual, Duration target) {
  return (actual.inSeconds - target.inSeconds).abs() <= 2;
}

class BuiltPlayerMedia {
  const BuiltPlayerMedia({required this.media, this.expiresAt});

  final Media media;
  final DateTime? expiresAt;
}

const List<double> playbackRateOptions = <double>[0.5, 1, 1.25, 1.5, 2];

double normalizePlaybackRate(double value) {
  for (final option in playbackRateOptions) {
    if (option == value) {
      return option;
    }
  }
  return 1;
}

String playbackRateLabel(double rate) {
  final normalized = normalizePlaybackRate(rate);
  if (normalized == normalized.roundToDouble()) {
    return '${normalized.toInt()}x';
  }
  return '${normalized}x';
}

Episode? nextEpisodeOf(List<Episode> episodes, int episodeId) {
  final index = episodes.indexWhere((episode) => episode.id == episodeId);
  if (index < 0 || index + 1 >= episodes.length) {
    return null;
  }
  return episodes[index + 1];
}

/// Dispose / 未起播的 0 秒哨兵不得写入 outbox 或上报，以免覆盖已有正进度。
bool shouldQueueProgress(int positionSeconds) => positionSeconds >= 1;

Future<BuiltPlayerMedia> buildPlayerMedia({
  required Future<MediaTokenResult> Function(int episodeId) requestMediaToken,
  required String serverUrl,
  required int episodeId,
  required int startPositionSeconds,
}) async {
  final result = await requestMediaToken(episodeId);
  DateTime? expiresAt;
  final raw = result.expiresAt.trim();
  if (raw.isNotEmpty) {
    expiresAt = DateTime.tryParse(raw);
  }
  final url = buildStreamUrlWithMediaToken(serverUrl, episodeId, result.token);
  return BuiltPlayerMedia(
    media: Media(
      url,
      start: startPositionSeconds > 0
          ? Duration(seconds: startPositionSeconds)
          : null,
    ),
    expiresAt: expiresAt,
  );
}

double _clamp01(double value) => value.clamp(0.0, 1.0).toDouble();

Duration _clampDuration(Duration value, Duration duration) {
  if (value < Duration.zero) {
    return Duration.zero;
  }
  if (duration > Duration.zero && value > duration) {
    return duration;
  }
  return value;
}
