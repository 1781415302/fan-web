import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:media_kit/media_kit.dart' show SubtitleTrack;
import 'package:media_kit_video/media_kit_video.dart';

import '../providers/auth_provider.dart';
import '../providers/player_provider.dart';
import '../theme/app_theme.dart';

class PlayerScreen extends ConsumerStatefulWidget {
  const PlayerScreen({
    required this.animeId,
    required this.episodeId,
    this.animeTitle,
    this.episodeNumber,
    super.key,
  });

  final int animeId;
  final int episodeId;
  final String? animeTitle;
  final int? episodeNumber;

  @override
  ConsumerState<PlayerScreen> createState() => _PlayerScreenState();
}

class _PlayerScreenState extends ConsumerState<PlayerScreen>
    with WidgetsBindingObserver {
  PlayerConfig? _config;
  PlayerNotifier? _notifier;
  final GlobalKey<VideoState> _videoKey = GlobalKey<VideoState>();
  Timer? _controlsTimer;
  Timer? _hintTimer;
  bool _controlsVisible = true;
  bool _isExiting = false;
  _GestureAxis? _gestureAxis;
  bool _gestureLeftSide = false;
  bool _panGestureStarted = false;
  bool _dragWasPlaying = false;
  double _dragStartX = 0;
  double _dragStartY = 0;
  Duration _dragStartPosition = Duration.zero;
  Duration _dragTarget = Duration.zero;
  double _dragStartBrightness = 0.5;
  double _dragStartVolume = 1;
  String? _gestureHint;
  IconData? _gestureHintIcon;
  bool _sliderDragging = false;
  bool _sliderWasPlaying = false;
  Duration _sliderPosition = Duration.zero;
  int _controlsHideGeneration = 0;
  bool _initialControlsHideScheduled = false;
  DateTime? _controlsShownAt;
  EdgeInsets? _requestedSubtitlePadding;

  static const _controlsAutoHideDelay = Duration(seconds: 3);

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    final authState = ref.read(authProvider);
    final serverUrl = authState.serverUrl;
    final token = authState.token;
    if (serverUrl != null &&
        serverUrl.isNotEmpty &&
        token != null &&
        token.isNotEmpty) {
      _config = PlayerConfig(
        animeId: widget.animeId,
        episodeId: widget.episodeId,
        serverUrl: serverUrl,
        token: token,
        animeTitle: widget.animeTitle,
        episodeNumber: widget.episodeNumber,
      );
    }
    unawaited(
      SystemChrome.setEnabledSystemUIMode(SystemUiMode.immersiveSticky),
    );
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState lifecycleState) {
    if (lifecycleState == AppLifecycleState.paused ||
        lifecycleState == AppLifecycleState.inactive ||
        lifecycleState == AppLifecycleState.hidden ||
        lifecycleState == AppLifecycleState.detached) {
      final notifier = _notifier;
      if (notifier != null) {
        unawaited(notifier.pauseAndReport());
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final config = _config;
    if (config == null) {
      return const _PlayerUnavailableScreen();
    }

    final provider = playerProvider(config);
    final playerState = ref.watch(provider);
    final notifier = ref.read(provider.notifier);
    _notifier = notifier;
    ref.listen<PlayerState>(provider, (previous, next) {
      _maybeScheduleInitialControlsHide(next);
      final restoredPosition = next.restoredPosition;
      if (restoredPosition == null) {
        return;
      }
      notifier.consumeRestoredPosition();
      if (!mounted) {
        return;
      }
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted) {
          return;
        }
        ScaffoldMessenger.of(context)
          ..hideCurrentSnackBar()
          ..showSnackBar(
            SnackBar(content: Text('已恢复到 ${formatTime(restoredPosition)}')),
          );
      });
    });
    _maybeScheduleInitialControlsHide(playerState);

    final isLandscape =
        MediaQuery.orientationOf(context) == Orientation.landscape;
    final subtitlePadding = EdgeInsets.fromLTRB(
      16,
      0,
      16,
      _controlsVisible ? (isLandscape ? 64 : 132) : (isLandscape ? 16 : 24),
    );
    _syncSubtitlePadding(subtitlePadding);

    return PopScope<void>(
      canPop: false,
      onPopInvokedWithResult: (didPop, result) {
        if (!didPop) {
          unawaited(_exit(notifier));
        }
      },
      child: Scaffold(
        backgroundColor: Colors.black,
        resizeToAvoidBottomInset: false,
        body: Stack(
          fit: StackFit.expand,
          children: [
            Video(
              key: _videoKey,
              controller: notifier.videoController,
              fit: BoxFit.contain,
              fill: Colors.black,
              controls: null,
              subtitleViewConfiguration: SubtitleViewConfiguration(
                visible: playerState.currentSubtitleTrack != null,
                style: TextStyle(
                  height: 1.3,
                  fontSize: playerState.subtitleFontSize,
                  color: Colors.white,
                  fontWeight: FontWeight.w500,
                  backgroundColor: const Color(0xAA000000),
                ),
                textScaler: MediaQuery.textScalerOf(context),
                padding: subtitlePadding,
              ),
            ),
            Positioned.fill(child: _buildGestureLayer()),
            Positioned.fill(
              child: IgnorePointer(
                ignoring: !_controlsVisible,
                child: AnimatedOpacity(
                  opacity: _controlsVisible ? 1 : 0,
                  duration: const Duration(milliseconds: 180),
                  child: _buildControls(playerState, notifier),
                ),
              ),
            ),
            if (_gestureHint != null) _buildGestureHint(),
            if (playerState.isLoading) _buildLoadingOverlay(),
            if (playerState.error != null)
              _buildErrorOverlay(playerState.error!),
          ],
        ),
      ),
    );
  }

  Widget _buildGestureLayer() {
    final isLandscape =
        MediaQuery.orientationOf(context) == Orientation.landscape;
    final topControlInset = _controlsVisible
        ? (isLandscape ? 72.0 : 80.0)
        : 0.0;
    final bottomControlInset = _controlsVisible
        ? (isLandscape ? 128.0 : 168.0)
        : 0.0;

    return Padding(
      padding: EdgeInsets.only(
        top: topControlInset,
        bottom: bottomControlInset,
      ),
      child: GestureDetector(
        behavior: HitTestBehavior.opaque,
        onTap: _toggleControls,
        onDoubleTap: () {
          final notifier = _notifier;
          if (notifier == null) {
            return;
          }
          unawaited(_togglePlayback(notifier));
        },
        onPanStart: _onPanStart,
        onPanUpdate: _onPanUpdate,
        onPanEnd: _onPanEnd,
        onPanCancel: _onPanCancel,
      ),
    );
  }

  Widget _buildControls(PlayerState playerState, PlayerNotifier notifier) {
    final isLandscape =
        MediaQuery.orientationOf(context) == Orientation.landscape;
    final playButtonSize = isLandscape ? 44.0 : 52.0;

    return SafeArea(
      child: Column(
        children: [
          _buildControlsTouchRegion(
            child: ColoredBox(
              color: const Color(0xB3000000),
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                child: Row(
                  children: [
                    IconButton(
                      tooltip: '退出播放器',
                      onPressed: () => unawaited(_exit(notifier)),
                      icon: const Icon(Icons.arrow_back),
                      color: Colors.white,
                    ),
                    const SizedBox(width: 4),
                    Expanded(
                      child: Text(
                        _playerTitle,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          color: Colors.white,
                          fontSize: 15,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ),
                    IconButton(
                      tooltip: '字幕',
                      onPressed: playerState.subtitleTracks.isEmpty
                          ? null
                          : () => unawaited(
                              _showSubtitleSheet(playerState, notifier),
                            ),
                      icon: const Icon(Icons.subtitles_outlined),
                      color: Colors.white,
                    ),
                  ],
                ),
              ),
            ),
          ),
          const Spacer(),
          _buildControlsTouchRegion(
            child: ColoredBox(
              color: const Color(0xB3000000),
              child: Padding(
                padding: EdgeInsets.fromLTRB(
                  12,
                  isLandscape ? 4 : 8,
                  12,
                  isLandscape ? 6 : 10,
                ),
                child: Column(
                  children: [
                    _buildProgressSlider(
                      playerState,
                      notifier,
                      compact: isLandscape,
                    ),
                    Row(
                      children: [
                        SizedBox(
                          width: 52,
                          child: Text(
                            formatTime(
                              _sliderDragging
                                  ? _sliderPosition.inSeconds
                                  : playerState.position.inSeconds,
                            ),
                            style: const TextStyle(
                              color: Colors.white,
                              fontSize: 12,
                            ),
                          ),
                        ),
                        const Spacer(),
                        SizedBox(
                          width: 52,
                          child: Text(
                            formatTime(playerState.duration.inSeconds),
                            textAlign: TextAlign.right,
                            style: const TextStyle(
                              color: Colors.white70,
                              fontSize: 12,
                            ),
                          ),
                        ),
                      ],
                    ),
                    SizedBox(height: isLandscape ? 2 : 4),
                    SizedBox.square(
                      dimension: playButtonSize,
                      child: IconButton.filled(
                        tooltip: playerState.isPlaying ? '暂停' : '播放',
                        onPressed: playerState.error == null
                            ? () => unawaited(_togglePlayback(notifier))
                            : null,
                        icon: Icon(
                          playerState.isPlaying
                              ? Icons.pause
                              : Icons.play_arrow,
                          size: isLandscape ? 26 : 30,
                        ),
                        style: IconButton.styleFrom(
                          backgroundColor: AppTheme.accent,
                          foregroundColor: AppTheme.background,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildControlsTouchRegion({required Widget child}) {
    return Listener(
      behavior: HitTestBehavior.opaque,
      onPointerDown: (_) => _cancelControlsHide(),
      onPointerUp: (_) => _scheduleControlsHide(),
      onPointerCancel: (_) => _scheduleControlsHide(),
      child: child,
    );
  }

  Widget _buildProgressSlider(
    PlayerState playerState,
    PlayerNotifier notifier, {
    bool compact = false,
  }) {
    final maximum = playerState.duration.inMilliseconds <= 0
        ? 1.0
        : playerState.duration.inMilliseconds / 1000;
    final position = _sliderDragging
        ? _sliderPosition.inMilliseconds / 1000
        : playerState.position.inMilliseconds / 1000;
    final value = position.clamp(0.0, maximum).toDouble();
    return SliderTheme(
      data: SliderTheme.of(context).copyWith(
        trackHeight: 3,
        thumbShape: const RoundSliderThumbShape(enabledThumbRadius: 6),
        overlayShape: const RoundSliderOverlayShape(overlayRadius: 16),
        padding: compact ? EdgeInsets.zero : null,
        activeTrackColor: AppTheme.accent,
        inactiveTrackColor: Colors.white30,
        thumbColor: AppTheme.accent,
        overlayColor: AppTheme.accent.withValues(alpha: 0.22),
      ),
      child: SizedBox(
        height: compact ? 32 : 48,
        child: Slider(
          min: 0,
          max: maximum,
          value: value,
          onChangeStart: playerState.duration <= Duration.zero
              ? null
              : (_) {
                  _sliderDragging = true;
                  _sliderWasPlaying = playerState.isPlaying;
                  _sliderPosition = playerState.position;
                  if (_sliderWasPlaying) {
                    unawaited(notifier.pause());
                  }
                  _showControls();
                  setState(() {});
                },
          onChanged: playerState.duration <= Duration.zero
              ? null
              : (value) {
                  setState(() {
                    _sliderPosition = Duration(
                      milliseconds: (value * 1000).round(),
                    );
                  });
                },
          onChangeEnd: playerState.duration <= Duration.zero
              ? null
              : (value) {
                  final target = Duration(milliseconds: (value * 1000).round());
                  _sliderDragging = false;
                  unawaited(
                    _finishSliderDrag(notifier, target, _sliderWasPlaying),
                  );
                  setState(() {});
                },
        ),
      ),
    );
  }

  Widget _buildGestureHint() {
    return Center(
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: Colors.black.withValues(alpha: 0.72),
          borderRadius: BorderRadius.circular(10),
        ),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 14),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (_gestureHintIcon != null) ...[
                Icon(_gestureHintIcon, color: Colors.white, size: 24),
                const SizedBox(width: 10),
              ],
              Text(
                _gestureHint!,
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 16,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildLoadingOverlay() {
    return const ColoredBox(
      color: Color(0x66000000),
      child: Center(child: CircularProgressIndicator(color: AppTheme.accent)),
    );
  }

  Widget _buildErrorOverlay(String message) {
    return ColoredBox(
      color: const Color(0xCC000000),
      child: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(
                Icons.error_outline,
                color: AppTheme.destructive,
                size: 42,
              ),
              const SizedBox(height: 14),
              Text(
                message,
                textAlign: TextAlign.center,
                style: const TextStyle(color: Colors.white, height: 1.45),
              ),
              const SizedBox(height: 18),
              FilledButton.icon(
                onPressed: () => unawaited(_exit(_notifier)),
                icon: const Icon(Icons.arrow_back),
                label: const Text('返回详情'),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _exit(PlayerNotifier? notifier) async {
    if (_isExiting) {
      return;
    }
    _isExiting = true;
    _cancelControlsHide();
    if (notifier != null) {
      await notifier.pauseAndReport();
    }
    if (mounted) {
      context.pop();
    }
  }

  Future<void> _togglePlayback(PlayerNotifier notifier) async {
    if (notifier.currentState.isPlaying) {
      await notifier.pause();
    } else {
      await notifier.play();
    }
    _showControls();
  }

  void _toggleControls() {
    if (_controlsVisible) {
      _cancelControlsHide();
      _controlsShownAt = null;
      setState(() {
        _controlsVisible = false;
      });
    } else {
      _showControls();
    }
  }

  void _showControls() {
    if (!mounted) {
      return;
    }
    _cancelControlsHide();
    _controlsShownAt = DateTime.now();
    setState(() {
      _controlsVisible = true;
    });
    _scheduleControlsHide();
  }

  void _cancelControlsHide() {
    _controlsHideGeneration++;
    _controlsTimer?.cancel();
    _controlsTimer = null;
  }

  void _scheduleControlsHide() {
    if (!mounted || !_controlsVisible || _isExiting) {
      return;
    }
    final generation = ++_controlsHideGeneration;
    final shownAt = DateTime.now();
    _controlsShownAt = shownAt;
    _controlsTimer?.cancel();
    _controlsTimer = Timer(_controlsAutoHideDelay, () {
      if (!mounted ||
          !_controlsVisible ||
          generation != _controlsHideGeneration ||
          shownAt != _controlsShownAt) {
        return;
      }
      final elapsed = DateTime.now().difference(shownAt);
      final remaining = _controlsAutoHideDelay - elapsed;
      if (remaining > Duration.zero) {
        _controlsTimer = Timer(remaining, () {
          if (mounted &&
              _controlsVisible &&
              generation == _controlsHideGeneration &&
              shownAt == _controlsShownAt) {
            _controlsTimer = null;
            _controlsShownAt = null;
            setState(() {
              _controlsVisible = false;
            });
          }
        });
        return;
      }
      _controlsTimer = null;
      _controlsShownAt = null;
      setState(() {
        _controlsVisible = false;
      });
    });
  }

  void _maybeScheduleInitialControlsHide(PlayerState playerState) {
    if (_initialControlsHideScheduled ||
        playerState.isLoading ||
        playerState.error != null) {
      return;
    }
    _initialControlsHideScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && _controlsVisible) {
        _scheduleControlsHide();
      }
    });
  }

  void _syncSubtitlePadding(EdgeInsets padding) {
    if (_requestedSubtitlePadding == padding) {
      return;
    }
    _requestedSubtitlePadding = padding;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || _requestedSubtitlePadding != padding) {
        return;
      }
      _videoKey.currentState?.setSubtitleViewPadding(
        padding,
        duration: const Duration(milliseconds: 180),
      );
    });
  }

  void _onPanStart(DragStartDetails details) {
    final notifier = _notifier;
    if (notifier == null) {
      return;
    }
    _panGestureStarted = true;
    _cancelControlsHide();
    final state = notifier.currentState;
    _gestureAxis = null;
    _gestureLeftSide =
        details.localPosition.dx < MediaQuery.sizeOf(context).width / 2;
    _dragWasPlaying = state.isPlaying;
    _dragStartX = details.localPosition.dx;
    _dragStartY = details.localPosition.dy;
    _dragStartPosition = state.position;
    _dragTarget = state.position;
    _dragStartBrightness = state.brightness;
    _dragStartVolume = state.volume;
    _hintTimer?.cancel();
  }

  void _onPanUpdate(DragUpdateDetails details) {
    final notifier = _notifier;
    if (notifier == null) {
      return;
    }
    final dx = details.localPosition.dx - _dragStartX;
    final dy = details.localPosition.dy - _dragStartY;
    if (_gestureAxis == null) {
      final horizontalDelta = dx.abs();
      final verticalDelta = dy.abs();
      if (horizontalDelta < 4 && verticalDelta < 4) {
        return;
      }
      _gestureAxis = horizontalDelta >= verticalDelta
          ? _GestureAxis.horizontal
          : _GestureAxis.vertical;
      if (_gestureAxis == _GestureAxis.horizontal && _dragWasPlaying) {
        unawaited(notifier.pause());
      }
    }

    if (_gestureAxis == _GestureAxis.horizontal) {
      final duration = notifier.currentState.duration;
      if (duration <= Duration.zero) {
        return;
      }
      final width = MediaQuery.sizeOf(context).width;
      final deltaSeconds = width <= 0
          ? 0
          : dx / width * duration.inSeconds * 1.5;
      _dragTarget = _clampPlayerDuration(
        _dragStartPosition + Duration(seconds: deltaSeconds.round()),
        duration,
      );
      _setGestureHint(
        '${dx >= 0 ? '快进' : '快退'} ${formatTime(_dragTarget.inSeconds)}',
        dx >= 0 ? Icons.fast_forward : Icons.fast_rewind,
      );
      return;
    }

    final height = MediaQuery.sizeOf(context).height;
    final delta = height <= 0 ? 0.0 : -dy / height;
    if (_gestureLeftSide) {
      final brightness = _clamp01(_dragStartBrightness + delta);
      unawaited(notifier.setBrightness(brightness));
      _setGestureHint(
        '亮度 ${(brightness * 100).round()}%',
        Icons.brightness_6_outlined,
      );
    } else {
      final volume = _clamp01(_dragStartVolume + delta);
      unawaited(notifier.setVolume(volume));
      _setGestureHint(
        '音量 ${(volume * 100).round()}%',
        volume == 0 ? Icons.volume_off : Icons.volume_up,
      );
    }
  }

  void _onPanEnd(DragEndDetails details) {
    if (!_panGestureStarted) {
      return;
    }
    _panGestureStarted = false;
    final notifier = _notifier;
    final axis = _gestureAxis;
    if (notifier != null && axis == _GestureAxis.horizontal) {
      final target = _dragTarget;
      final wasPlaying = _dragWasPlaying;
      unawaited(_finishHorizontalDrag(notifier, target, wasPlaying));
    }
    _gestureAxis = null;
    _showControls();
  }

  void _onPanCancel() {
    if (!_panGestureStarted) {
      return;
    }
    _panGestureStarted = false;
    _gestureAxis = null;
    _showControls();
  }

  Future<void> _finishHorizontalDrag(
    PlayerNotifier notifier,
    Duration target,
    bool wasPlaying,
  ) async {
    await notifier.seek(target);
    if (wasPlaying) {
      await notifier.play();
    }
    _scheduleControlsHide();
  }

  Future<void> _finishSliderDrag(
    PlayerNotifier notifier,
    Duration target,
    bool wasPlaying,
  ) async {
    await notifier.seek(target);
    if (wasPlaying) {
      await notifier.play();
    }
    _showControls();
  }

  void _setGestureHint(String hint, IconData icon) {
    if (!mounted) {
      return;
    }
    _hintTimer?.cancel();
    setState(() {
      _gestureHint = hint;
      _gestureHintIcon = icon;
    });
    _hintTimer = Timer(const Duration(milliseconds: 700), () {
      if (mounted) {
        setState(() {
          _gestureHint = null;
          _gestureHintIcon = null;
        });
      }
    });
  }

  Future<void> _showSubtitleSheet(
    PlayerState playerState,
    PlayerNotifier notifier,
  ) async {
    _showControls();
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      backgroundColor: AppTheme.muted,
      showDragHandle: true,
      builder: (sheetContext) {
        var selectedTrack = playerState.currentSubtitleTrack;
        var selectedFontSize = playerState.subtitleFontSize;
        return StatefulBuilder(
          builder: (context, setSheetState) {
            return ConstrainedBox(
              constraints: BoxConstraints(
                maxHeight: MediaQuery.sizeOf(context).height * 0.92,
              ),
              child: SingleChildScrollView(
                padding: const EdgeInsets.fromLTRB(20, 0, 20, 20),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        const Expanded(
                          child: Text(
                            '字幕',
                            style: TextStyle(
                              color: AppTheme.foreground,
                              fontSize: 18,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ),
                        IconButton(
                          tooltip: '关闭',
                          onPressed: () => Navigator.of(sheetContext).pop(),
                          icon: const Icon(Icons.close),
                        ),
                      ],
                    ),
                    _SubtitleOption(
                      icon: Icons.subtitles_off_outlined,
                      title: '关闭字幕',
                      selected: selectedTrack == null,
                      onTap: () {
                        selectedTrack = null;
                        setSheetState(() {});
                        unawaited(notifier.setNoSubtitle());
                      },
                    ),
                    for (final track in playerState.subtitleTracks)
                      _SubtitleOption(
                        icon: Icons.subtitles_outlined,
                        title: _subtitleName(track),
                        selected: selectedTrack == track,
                        onTap: () {
                          selectedTrack = track;
                          setSheetState(() {});
                          unawaited(notifier.setSubtitleTrack(track));
                        },
                      ),
                    const SizedBox(height: 12),
                    const Text(
                      '字号',
                      style: TextStyle(
                        color: AppTheme.foreground,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    const SizedBox(height: 8),
                    SizedBox(
                      width: double.infinity,
                      child: SegmentedButton<double>(
                        showSelectedIcon: false,
                        segments: const [
                          ButtonSegment(value: 20, label: Text('小')),
                          ButtonSegment(value: 24, label: Text('中')),
                          ButtonSegment(value: 28, label: Text('大')),
                        ],
                        selected: {selectedFontSize},
                        onSelectionChanged: (values) {
                          final value = values.first;
                          selectedFontSize = value;
                          setSheetState(() {});
                          notifier.setSubtitleFontSize(value);
                        },
                      ),
                    ),
                  ],
                ),
              ),
            );
          },
        );
      },
    );
    _showControls();
  }

  String get _playerTitle {
    final title = _config?.animeTitle?.trim() ?? '';
    final episode = _config?.episodeNumber ?? widget.episodeId;
    return title.isEmpty ? '第$episode话' : '$title  第$episode话';
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _cancelControlsHide();
    _hintTimer?.cancel();
    unawaited(SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge));
    super.dispose();
  }
}

class _SubtitleOption extends StatelessWidget {
  const _SubtitleOption({
    required this.icon,
    required this.title,
    required this.selected,
    required this.onTap,
  });

  final IconData icon;
  final String title;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      contentPadding: EdgeInsets.zero,
      leading: Icon(icon, color: selected ? AppTheme.accent : Colors.white70),
      title: Text(title, maxLines: 1, overflow: TextOverflow.ellipsis),
      trailing: selected
          ? const Icon(Icons.check, color: AppTheme.accent)
          : null,
      onTap: onTap,
    );
  }
}

class _PlayerUnavailableScreen extends StatelessWidget {
  const _PlayerUnavailableScreen();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      body: Center(
        child: FilledButton.icon(
          onPressed: () => context.pop(),
          icon: const Icon(Icons.arrow_back),
          label: const Text('返回详情'),
        ),
      ),
    );
  }
}

enum _GestureAxis { horizontal, vertical }

String _subtitleName(SubtitleTrack track) {
  final title = track.title?.trim() ?? '';
  if (title.isNotEmpty) {
    return title;
  }
  final language = track.language?.trim() ?? '';
  if (language.isNotEmpty) {
    return language;
  }
  return '字幕 ${track.id}';
}

String formatTime(int seconds) {
  final safeSeconds = seconds < 0 ? 0 : seconds;
  final hours = safeSeconds ~/ 3600;
  final minutes = (safeSeconds % 3600) ~/ 60;
  final remainder = safeSeconds % 60;
  if (hours > 0) {
    return '${hours.toString().padLeft(2, '0')}:${minutes.toString().padLeft(2, '0')}:${remainder.toString().padLeft(2, '0')}';
  }
  return '${minutes.toString().padLeft(2, '0')}:${remainder.toString().padLeft(2, '0')}';
}

Duration _clampPlayerDuration(Duration value, Duration duration) {
  if (value < Duration.zero) {
    return Duration.zero;
  }
  if (duration > Duration.zero && value > duration) {
    return duration;
  }
  return value;
}

double _clamp01(double value) => value.clamp(0.0, 1.0).toDouble();
