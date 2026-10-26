import 'package:flutter/scheduler.dart';
import 'package:flutter/widgets.dart';

import '../models/playback_interaction.dart';

/// 播放画面手势层：长按临时倍速 + 既有 tap/doubleTap/pan 转发。
///
/// 外层 Listener 跟踪实际指针序列，负责可靠结束临时倍速（长按成功后的
/// PointerCancel 不保证触发 onLongPressCancel）；内层 GestureDetector 保留
/// Flutter 原生手势竞争。本组件不依赖 Provider、Player 或进度逻辑。
class PlayerGestureSurface extends StatefulWidget {
  const PlayerGestureSurface({
    required this.sessionIdentity,
    required this.enabled,
    required this.boostEnabled,
    required this.onBoostStart,
    required this.onTap,
    required this.onDoubleTap,
    required this.onPanStart,
    required this.onPanUpdate,
    required this.onPanEnd,
    required this.onPanCancel,
    required this.onInteractionStart,
    required this.onInteractionEnd,
    super.key,
  });

  final Object sessionIdentity;
  final bool enabled, boostEnabled;
  final TemporaryBoostSession? Function() onBoostStart;
  final VoidCallback onTap, onDoubleTap;
  final GestureDragStartCallback onPanStart;
  final GestureDragUpdateCallback onPanUpdate;
  final GestureDragEndCallback onPanEnd;
  final GestureDragCancelCallback onPanCancel;
  final VoidCallback onInteractionStart, onInteractionEnd;

  @override
  State<PlayerGestureSurface> createState() => _PlayerGestureSurfaceState();
}

class _PlayerGestureSurfaceState extends State<PlayerGestureSurface> {
  /// 当前触摸序列中按下的指针。
  final Set<int> _pointers = <int>{};

  /// 每个新触摸序列递增，用于丢弃延后执行的旧序列收尾。
  int _sequenceToken = 0;

  /// 本序列已调用 onInteractionStart、尚未调用 onInteractionEnd。
  bool _sequenceOpen = false;

  /// 本序列被抑制（多指、禁用、会话更换等），剩余手势事件全部忽略。
  bool _suppressed = false;

  /// 长按已在手势竞争中获胜（不论是否真正开始加速）。
  bool _longPressWon = false;

  /// 保留原 _panGestureStarted 防护：仅已开始的 pan 才转发结束/取消。
  bool _panGestureStarted = false;

  TemporaryBoostSession? _boost;
  bool _disposed = false;

  bool get _acceptsGesture => widget.enabled && !_suppressed;

  @override
  void didUpdateWidget(covariant PlayerGestureSurface oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!widget.enabled ||
        !identical(widget.sessionIdentity, oldWidget.sessionIdentity)) {
      _invalidateSequenceDeferred();
      return;
    }
    // boostEnabled 只管临时倍速资格：仅在已有 active boost 时结束并抑制。
    if (!widget.boostEnabled && _boost != null) {
      _invalidateSequenceDeferred();
    }
  }

  @override
  void deactivate() {
    _invalidateSequenceDeferred();
    super.deactivate();
  }

  @override
  void dispose() {
    _disposed = true;
    _invalidateSequenceDeferred();
    super.dispose();
  }

  /// widget 生命周期触发的失效：同步解绑并屏蔽旧事件，把旧句柄 end 和父级
  /// 收尾延后到本帧之后，避免在 build/卸载过程中同步写 Riverpod state。
  void _invalidateSequenceDeferred() {
    final handle = _boost;
    final panWasStarted = _panGestureStarted;
    final hadOpenSequence = _sequenceOpen;
    final token = _sequenceToken;
    _boost = null;
    _panGestureStarted = false;
    _sequenceOpen = false;
    if (_pointers.isNotEmpty) {
      _suppressed = true;
    }
    if (handle == null && !panWasStarted && !hadOpenSequence) {
      return;
    }
    // 捕获当前回调，不在稍后重新查找（可能已属于新会话）。
    final onPanCancel = widget.onPanCancel;
    final onInteractionEnd = widget.onInteractionEnd;
    SchedulerBinding.instance.addPostFrameCallback((_) {
      handle?.end();
      final stillCurrent = !_disposed && mounted && token == _sequenceToken;
      if (!stillCurrent) {
        return;
      }
      if (panWasStarted) {
        onPanCancel();
      }
      if (hadOpenSequence) {
        onInteractionEnd();
      }
    });
    SchedulerBinding.instance.ensureVisualUpdate();
  }

  /// 指针事件触发的结束：立即执行，幂等。
  void _endBoostNow() {
    final handle = _boost;
    _boost = null;
    handle?.end();
  }

  void _finishSequenceNow() {
    _endBoostNow();
    if (_sequenceOpen) {
      _sequenceOpen = false;
      widget.onInteractionEnd();
    }
  }

  void _handlePointerDown(PointerDownEvent event) {
    if (_pointers.isEmpty) {
      // 新触摸序列。
      _sequenceToken++;
      _suppressed = !widget.enabled;
      _longPressWon = false;
      _panGestureStarted = false;
      _boost = null;
      _pointers.add(event.pointer);
      if (widget.enabled) {
        _sequenceOpen = true;
        widget.onInteractionStart();
      }
      return;
    }
    _pointers.add(event.pointer);
    // 第二根手指：结束 boost 并抑制本序列，已开始的 pan 发送一次 cancel。
    if (!_suppressed) {
      _suppressed = true;
      _endBoostNow();
      if (_panGestureStarted) {
        _panGestureStarted = false;
        widget.onPanCancel();
      }
    }
  }

  void _handlePointerEnd(PointerEvent event) {
    if (!_pointers.remove(event.pointer)) {
      return;
    }
    if (_pointers.isEmpty) {
      _finishSequenceNow();
    } else {
      _endBoostNow();
    }
  }

  void _handleLongPressStart(LongPressStartDetails details) {
    if (!_acceptsGesture) {
      return;
    }
    _longPressWon = true;
    if (!widget.boostEnabled || _boost != null) {
      return;
    }
    _boost = widget.onBoostStart();
  }

  void _handleLongPressMoveUpdate(LongPressMoveUpdateDetails details) {
    if (!_acceptsGesture || !_longPressWon) {
      return;
    }
    // 以长按起点为基点的累计水平位移；垂直位移不影响音量/亮度。
    _boost?.updateDisplacement(details.localOffsetFromOrigin.dx);
  }

  void _handleLongPressEnd(LongPressEndDetails details) {
    _endBoostNow();
  }

  void _handleLongPressCancel() {
    // 长按尚未成功即失败：没有 boost，也不触发 pan 完成。
    _endBoostNow();
  }

  void _handleTap() {
    if (!_acceptsGesture || _longPressWon) {
      return;
    }
    widget.onTap();
  }

  void _handleDoubleTap() {
    if (!_acceptsGesture || _longPressWon) {
      return;
    }
    widget.onDoubleTap();
  }

  void _handlePanStart(DragStartDetails details) {
    if (!_acceptsGesture || _longPressWon) {
      return;
    }
    _panGestureStarted = true;
    widget.onPanStart(details);
  }

  void _handlePanUpdate(DragUpdateDetails details) {
    if (!_panGestureStarted || _suppressed) {
      return;
    }
    widget.onPanUpdate(details);
  }

  void _handlePanEnd(DragEndDetails details) {
    if (!_panGestureStarted || _suppressed) {
      return;
    }
    _panGestureStarted = false;
    widget.onPanEnd(details);
  }

  void _handlePanCancel() {
    // 长按获胜导致 pan recognizer 被拒绝，或 tap：未开始的 pan 不转发。
    if (!_panGestureStarted || _suppressed) {
      return;
    }
    _panGestureStarted = false;
    widget.onPanCancel();
  }

  @override
  Widget build(BuildContext context) {
    return Listener(
      behavior: HitTestBehavior.opaque,
      onPointerDown: _handlePointerDown,
      onPointerUp: _handlePointerEnd,
      onPointerCancel: _handlePointerEnd,
      child: GestureDetector(
        behavior: HitTestBehavior.opaque,
        onTap: _handleTap,
        onDoubleTap: _handleDoubleTap,
        onLongPressStart: _handleLongPressStart,
        onLongPressMoveUpdate: _handleLongPressMoveUpdate,
        onLongPressEnd: _handleLongPressEnd,
        onLongPressCancel: _handleLongPressCancel,
        onPanStart: _handlePanStart,
        onPanUpdate: _handlePanUpdate,
        onPanEnd: _handlePanEnd,
        onPanCancel: _handlePanCancel,
      ),
    );
  }
}
