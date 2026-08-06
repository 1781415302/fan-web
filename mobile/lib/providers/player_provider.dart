import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';
import 'package:screen_brightness/screen_brightness.dart';

import '../api/progress_api.dart';
import 'anime_provider.dart';

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
    this.subtitleFontSize = 24,
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
    return PlayerState(
      isInitialized: true,
      isLoading: true,
      volume: _clamp01(player.state.volume / 100),
    );
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
      await player.open(
        Media(buildStreamUrl(config.serverUrl, config.episodeId, config.token)),
        play: false,
      );
      if (_disposed || _disposing) {
        return;
      }
      _hasOpened = true;
      _openCompleted = true;
      _startProgressTimer();
      final duration = player.state.duration;
      if (duration > Duration.zero) {
        await _restoreAndStart(duration);
      } else if (_savedPosition == 0) {
        _restoreHandled = true;
        _setState(state.copyWith(isRestoring: false));
        await _startPlaybackIfReady();
      }
    } catch (_) {
      _setState(state.copyWith(isLoading: false, error: '播放失败，请检查网络或重新登录'));
      return;
    } finally {
      _setState(state.copyWith(isLoading: false));
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
    if (duration > Duration.zero) {
      unawaited(_restoreAndStart(duration));
    }
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
    _setState(
      state.copyWith(
        isLoading: false,
        error: message.trim().isEmpty ? '播放失败，请检查网络或重新登录' : message,
      ),
    );
  }

  Future<void> _restoreAndStart(Duration duration) async {
    if (_disposed || _disposing) {
      return;
    }
    if (!_restoreHandled) {
      if (_savedPosition <= 0) {
        _restoreHandled = true;
        _setState(state.copyWith(isRestoring: false));
      } else if (duration > Duration.zero) {
        _restoreHandled = true;
        if (_savedPosition < duration.inSeconds) {
          final target = Duration(seconds: _savedPosition);
          try {
            await player.seek(target);
          } catch (_) {
            // seek 失败时从当前解码位置继续，不阻塞播放。
          }
          _setState(
            state.copyWith(
              position: target,
              isRestoring: false,
              restoredPosition: _savedPosition,
            ),
          );
        } else {
          _setState(state.copyWith(isRestoring: false));
        }
      }
    }
    await _startPlaybackIfReady();
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
      _setState(state.copyWith(error: '跳转失败，请稍后重试'));
    }
  }

  Future<void> setVolume(double value) async {
    if (_disposed || _disposing) {
      return;
    }
    final normalized = _clamp01(value);
    try {
      await player.setVolume(normalized * 100);
      _setState(state.copyWith(volume: normalized));
    } catch (_) {
      // 某些平台不支持设置音量时保持播放不中断。
    }
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
      // 桌面平台或无窗口绑定时忽略亮度设置失败。
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
      _setState(state.copyWith(error: '字幕切换失败'));
    }
  }

  Future<void> setNoSubtitle() => setSubtitleTrack(SubtitleTrack.no());

  void setSubtitleFontSize(double value) {
    if (_disposed || _disposing) {
      return;
    }
    _setState(state.copyWith(subtitleFontSize: value.clamp(20, 28).toDouble()));
  }

  void consumeRestoredPosition() {
    if (_disposed || state.restoredPosition == null) {
      return;
    }
    _setState(state.copyWith(clearRestoredPosition: true));
  }

  Future<void> _loadBrightness() async {
    try {
      final value = await ScreenBrightness.instance.application;
      if (!_disposed && !_disposing) {
        _setState(state.copyWith(brightness: _clamp01(value)));
      }
    } catch (_) {
      // 读取失败时使用默认亮度，不影响播放器。
    }
  }

  Future<void> _queueCurrentProgress({bool forceWatched = false}) {
    return _queueProgressReport(
      position: state.position,
      watched: forceWatched || _isWatched(state.position),
    );
  }

  Future<void> _queueProgressReport({
    required Duration position,
    required bool watched,
  }) {
    if (_disposed && !_disposing) {
      return Future<void>.value();
    }
    if (!_hasOpened && !_disposing) {
      return Future<void>.value();
    }
    final safePosition = position.inSeconds < 0 ? 0 : position.inSeconds;
    final previous = _reportChain ?? Future<void>.value();
    final next = previous.then<void>((_) async {
      try {
        await _progressApi.reportProgress(
          config.episodeId,
          safePosition,
          watched,
        );
      } catch (_) {
        // 上报失败不打断播放或后续上报。
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
    for (final subscription in _subscriptions) {
      await subscription.cancel();
    }
    _subscriptions.clear();

    if (_hasOpened) {
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

String buildStreamUrl(String serverUrl, int episodeId, String token) {
  final normalized = serverUrl.trim().replaceFirst(RegExp(r'/+$'), '');
  return '$normalized/api/episodes/$episodeId/stream?token=${Uri.encodeComponent(token)}';
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
