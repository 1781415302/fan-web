import 'dart:async';

import 'package:flutter/widgets.dart' show AppLifecycleState;

import '../models/playback_interaction.dart';

/// 速率写入失败时的提示文案。
const String _rateFailureNotice = '倍速切换失败';

/// 一次待写/在途原生速率请求。
enum _RateKind { base, temporary, recovery }

class _RateRequest {
  const _RateRequest({
    required this.rate,
    required this.kind,
    required this.seq,
    this.session,
  });

  final double rate;
  final _RateKind kind;
  final int seq;
  final _BoostSession? session;
}

/// 绑定创建它的控制器实例与 id 的临时倍速句柄。
class _BoostSession implements TemporaryBoostSession {
  _BoostSession(this._owner, this.id);

  final PlaybackInteractionController _owner;

  @override
  final int id;

  @override
  void updateDisplacement(double horizontalDx) {
    _owner._updateBoost(this, horizontalDx);
  }

  @override
  void end() {
    _owner._endBoost(this);
  }
}

/// 单个播放会话的交互策略：基准/临时速率、串行速率通道、手势句柄、
/// 生命周期与播放意图。不持有播放器，只通过注入端口写速率与暂停。
class PlaybackInteractionController {
  PlaybackInteractionController({
    required this._writeRate,
    required this._pauseAndReport,
    required this._onRateChanged,
    required this._onNotice,
    double initialBaseRate = 1,
    AppLifecycleState initialLifecycleState = AppLifecycleState.resumed,
  }) : _baseRate = normalizePlaybackRate(initialBaseRate),
       _lifecycleState = initialLifecycleState {
    _lastSuccessfulBase = _baseRate;
    // 初始已处于真正后台时禁止自动起播。
    if (_isBackgroundState(initialLifecycleState)) {
      _background = true;
      _playIntent = false;
    }
  }

  final Future<void> Function(double rate) _writeRate;
  final Future<void> Function() _pauseAndReport;
  final void Function(PlaybackRateSnapshot snapshot) _onRateChanged;
  final void Function(String message) _onNotice;

  bool _closed = false;

  // ── 速率状态 ──
  double _baseRate;
  double? _temporaryRate;
  _BoostSession? _activeSession;
  int _nextSessionId = 0;

  // ── 速率通道 ──
  /// 首次 open 成功前、重开期间挂起：不派发原生 rate。
  bool _rateSuspended = true;
  bool _mediaReady = false;
  late double _lastSuccessfulBase;
  _RateRequest? _pendingRequest;
  _RateRequest? _inFlightRequest;
  Completer<void>? _inFlightDone;
  int _targetSeq = 0;
  final List<Completer<void>> _flushWaiters = <Completer<void>>[];

  // ── 生命周期与播放意图 ──
  AppLifecycleState _lifecycleState;
  bool _background = false;
  bool _playIntent = true;
  bool _pendingExplicitPlay = false;
  int _intentRevision = 0;

  PlaybackRateSnapshot get rate => PlaybackRateSnapshot(
    baseRate: _baseRate,
    temporaryRate: _temporaryRate,
    boostSessionId: _temporaryRate == null ? null : _activeSession?.id,
  );

  int get intentRevision => _intentRevision;

  bool get mayPlay => !_closed && _mediaReady && !_background && _playIntent;

  bool shouldPlayAfterMediaOpen({
    required bool wasPlaying,
    required int capturedIntentRevision,
  }) {
    if (!mayPlay) {
      return false;
    }
    if (wasPlaying && capturedIntentRevision == _intentRevision) {
      return true;
    }
    return _pendingExplicitPlay;
  }

  bool requestPlay({int? expectedIntentRevision}) {
    if (_closed || _background) {
      return false;
    }
    // 条件续播：版本必须完全一致，否则不改意图、不建新请求。
    if (expectedIntentRevision != null &&
        expectedIntentRevision != _intentRevision) {
      return false;
    }
    _playIntent = true;
    _intentRevision++;
    if (!_mediaReady) {
      // 引擎尚未就绪：记待执行，不能提前 play。
      _pendingExplicitPlay = true;
      return false;
    }
    return true;
  }

  void requestPause() {
    if (_closed) {
      return;
    }
    _clearPlayIntent();
    _cancelBoostInternal();
  }

  void onPlayingChanged(bool playing) {
    if (_closed) {
      return;
    }
    if (playing) {
      _pendingExplicitPlay = false;
    } else {
      // 只撤销 boost，不把内部 open(play:false) 当成用户暂停。
      _cancelBoostInternal();
    }
  }

  void setBaseRate(double value) {
    if (_closed) {
      return;
    }
    final normalized = normalizePlaybackRate(value);
    // 新的正常选择优先：结束当前 boost，使旧句柄失效。
    _activeSession = null;
    _temporaryRate = null;
    _baseRate = normalized;
    _publish();
    _enqueue(normalized, _RateKind.base);
  }

  TemporaryBoostSession? beginBoost({required bool eligible}) {
    if (!eligible ||
        _closed ||
        !_mediaReady ||
        _rateSuspended ||
        _background ||
        _lifecycleState != AppLifecycleState.resumed ||
        _activeSession != null) {
      return null;
    }
    final session = _BoostSession(this, ++_nextSessionId);
    _activeSession = session;
    _temporaryRate = defaultTemporaryPlaybackRate;
    _publish();
    _enqueue(
      defaultTemporaryPlaybackRate,
      _RateKind.temporary,
      session: session,
    );
    return session;
  }

  void cancelBoost() {
    if (_closed) {
      return;
    }
    _cancelBoostInternal();
  }

  Future<void> handleLifecycleState(AppLifecycleState state) async {
    if (_closed) {
      return;
    }
    _lifecycleState = state;
    switch (state) {
      case AppLifecycleState.resumed:
        // 只恢复可见性，不 play，不恢复已结束的 boost。
        _background = false;
      case AppLifecycleState.inactive:
        // 仍可见：结束 boost 恢复基准，不 pause；后台标志只由 resumed 清除。
        _cancelBoostInternal();
      case AppLifecycleState.hidden:
      case AppLifecycleState.paused:
      case AppLifecycleState.detached:
        _cancelBoostInternal();
        if (_background) {
          // 同一后台序列不重复暂停。
          return;
        }
        _background = true;
        // 同步先置暂停意图、递增版本，再发起异步暂停。
        _clearPlayIntent();
        try {
          await _pauseAndReport();
        } catch (_) {
          // 暂停端口异常在控制器内吸收，避免未捕获异常。
        }
    }
  }

  Future<void> suspendForMediaOpen() async {
    if (_closed) {
      return;
    }
    _rateSuspended = true;
    _mediaReady = false;
    _pendingRequest = null;
    _cancelBoostInternal();
    _completeFlushWaitersIfIdle();
    final done = _inFlightDone;
    if (done != null) {
      await done.future;
    }
  }

  Future<void> resumeAfterMediaOpen() async {
    if (_closed) {
      return;
    }
    _rateSuspended = false;
    _mediaReady = true;
    // 强制应用最新基准，即使数值与先前相同。
    _enqueue(_baseRate, _RateKind.base);
  }

  void mediaOpenFailed() {
    if (_closed) {
      return;
    }
    _rateSuspended = true;
    _mediaReady = false;
    _pendingRequest = null;
    _cancelBoostInternal();
    _completeFlushWaitersIfIdle();
  }

  Future<void> flushRates() {
    if (_closed || _rateSuspended) {
      return Future<void>.value();
    }
    if (_inFlightRequest == null && _pendingRequest == null) {
      return Future<void>.value();
    }
    final waiter = Completer<void>();
    _flushWaiters.add(waiter);
    return waiter.future;
  }

  void dispose() {
    if (_closed) {
      return;
    }
    _closed = true;
    _activeSession = null;
    _temporaryRate = null;
    _pendingRequest = null;
    _pendingExplicitPlay = false;
    _playIntent = false;
    for (final waiter in _flushWaiters) {
      if (!waiter.isCompleted) {
        waiter.complete();
      }
    }
    _flushWaiters.clear();
    final done = _inFlightDone;
    if (done != null && !done.isCompleted) {
      done.complete();
    }
  }

  // ── 内部实现 ──

  static bool _isBackgroundState(AppLifecycleState state) =>
      state == AppLifecycleState.hidden ||
      state == AppLifecycleState.paused ||
      state == AppLifecycleState.detached;

  void _clearPlayIntent() {
    _playIntent = false;
    _pendingExplicitPlay = false;
    _intentRevision++;
  }

  void _updateBoost(_BoostSession session, double horizontalDx) {
    if (_closed || !identical(_activeSession, session)) {
      return;
    }
    final next = temporaryRateForDisplacement(horizontalDx);
    if (next == _temporaryRate) {
      return;
    }
    _temporaryRate = next;
    _publish();
    _enqueue(next, _RateKind.temporary, session: session);
  }

  void _endBoost(_BoostSession session) {
    if (_closed || !identical(_activeSession, session)) {
      return;
    }
    _cancelBoostInternal();
  }

  /// 清临时态并发布基准目标；无 boost 时幂等。
  void _cancelBoostInternal() {
    if (_activeSession == null && _temporaryRate == null) {
      return;
    }
    _activeSession = null;
    _temporaryRate = null;
    _publish();
    _enqueue(_baseRate, _RateKind.base);
  }

  void _publish() {
    if (_closed) {
      return;
    }
    // 不吞掉发布回调的编程错误。
    _onRateChanged(rate);
  }

  void _enqueue(double target, _RateKind kind, {_BoostSession? session}) {
    if (_closed) {
      return;
    }
    _targetSeq++;
    if (_rateSuspended) {
      // 挂起期间只保留基准选择，resume 时强制应用。
      return;
    }
    // 只保留一个最新目标，取代旧待写目标。
    _pendingRequest = _RateRequest(
      rate: target,
      kind: kind,
      seq: _targetSeq,
      session: session,
    );
    _pump();
  }

  void _pump() {
    if (_closed ||
        _rateSuspended ||
        _inFlightRequest != null ||
        _pendingRequest == null) {
      return;
    }
    final request = _pendingRequest!;
    _pendingRequest = null;
    _inFlightRequest = request;
    final done = Completer<void>();
    _inFlightDone = done;
    Future<void>.sync(() => _writeRate(request.rate)).then(
      (_) => _onWriteDone(request, done, succeeded: true),
      onError: (Object _) => _onWriteDone(request, done, succeeded: false),
    );
  }

  void _onWriteDone(
    _RateRequest request,
    Completer<void> done, {
    required bool succeeded,
  }) {
    if (_closed) {
      // 已销毁：允许原生 Future 结束，但不回调、不再派发。
      if (!done.isCompleted) {
        done.complete();
      }
      return;
    }
    _inFlightRequest = null;
    _inFlightDone = null;
    if (succeeded) {
      // 只更新内部成功记录，不用旧 rate 覆盖当前快照。
      if (request.kind != _RateKind.temporary) {
        _lastSuccessfulBase = request.rate;
      }
    } else {
      _handleWriteFailure(request);
    }
    if (!done.isCompleted) {
      done.complete();
    }
    _pump();
    _completeFlushWaitersIfIdle();
  }

  void _handleWriteFailure(_RateRequest request) {
    final superseded = request.seq != _targetSeq || _pendingRequest != null;
    if (superseded || _rateSuspended) {
      // 过期请求失败：不回滚新状态、不提示，继续最新目标。
      return;
    }
    switch (request.kind) {
      case _RateKind.recovery:
        // 恢复再次失败：保留已有提示，不重复提示、不循环重试。
        return;
      case _RateKind.temporary:
        if (!identical(_activeSession, request.session)) {
          return;
        }
        _activeSession = null;
        _temporaryRate = null;
        _publish();
        _onNotice(_rateFailureNotice);
        _enqueue(_baseRate, _RateKind.recovery);
      case _RateKind.base:
        final previous = _baseRate;
        _baseRate = _lastSuccessfulBase;
        if (previous != _baseRate) {
          _publish();
        }
        _onNotice(_rateFailureNotice);
        _enqueue(_baseRate, _RateKind.recovery);
    }
  }

  void _completeFlushWaitersIfIdle() {
    // 挂起/closed 时不再派发，等待者立即放行；可写时等队列收敛。
    final converged = _inFlightRequest == null && _pendingRequest == null;
    if (!_closed && !_rateSuspended && !converged) {
      return;
    }
    for (final waiter in _flushWaiters) {
      if (!waiter.isCompleted) {
        waiter.complete();
      }
    }
    _flushWaiters.clear();
  }
}
