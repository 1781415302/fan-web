/// 播放交互共享契约：正常/临时速率常量、纯速率函数、速率快照与临时倍速句柄接口。
///
/// 本文件不依赖播放器、Provider 或网络，供交互控制器、手势组件与 Notifier 共用。
library;

/// 正常倍速菜单五档。
const List<double> playbackRateOptions = <double>[0.5, 1, 1.25, 1.5, 2];

/// 长按临时倍速五档。
const List<double> temporaryPlaybackRateOptions = <double>[
  1.25,
  1.5,
  2,
  2.5,
  3,
];

/// 每次长按的起始临时速率。
const double defaultTemporaryPlaybackRate = 2;

/// 水平拖动每跨一档所需的逻辑像素。
const double temporaryRateStepDistance = 48; // logical pixels

/// 归一化为正常五档之一，未知值返回 1。
double normalizePlaybackRate(double value) {
  for (final option in playbackRateOptions) {
    if (option == value) {
      return option;
    }
  }
  return 1;
}

/// 正常菜单速率文案，先按正常五档归一化。
String playbackRateLabel(double rate) {
  final normalized = normalizePlaybackRate(rate);
  return _formatRate(normalized);
}

/// 以长按起点为 0 的累计水平位移映射为临时速率；越界停在端点，非有限值回到 2x。
double temporaryRateForDisplacement(double horizontalDx) {
  final defaultIndex = temporaryPlaybackRateOptions.indexOf(
    defaultTemporaryPlaybackRate,
  );
  if (!horizontalDx.isFinite) {
    return defaultTemporaryPlaybackRate;
  }
  final steps = (horizontalDx / temporaryRateStepDistance).round();
  final index = (defaultIndex + steps).clamp(
    0,
    temporaryPlaybackRateOptions.length - 1,
  );
  return temporaryPlaybackRateOptions[index];
}

/// 临时速率文案；不在临时五档内的值回到 2x。
String temporaryPlaybackRateLabel(double rate) {
  for (final option in temporaryPlaybackRateOptions) {
    if (option == rate) {
      return _formatRate(option);
    }
  }
  return _formatRate(defaultTemporaryPlaybackRate);
}

String _formatRate(double rate) {
  if (rate == rate.roundToDouble()) {
    return '${rate.toInt()}x';
  }
  return '${rate}x';
}

/// 当前请求的速率交互状态快照（不可变）。
///
/// temporaryRate 与 boostSessionId 同时为 null 或同时非 null。
class PlaybackRateSnapshot {
  const PlaybackRateSnapshot({
    required this.baseRate,
    required this.temporaryRate,
    required this.boostSessionId,
  });

  final double baseRate;
  final double? temporaryRate;
  final int? boostSessionId;

  double get effectiveRate => temporaryRate ?? baseRate;
}

/// 一次长按临时倍速的句柄；失效后 update/end 均无副作用。
abstract interface class TemporaryBoostSession {
  int get id;

  /// 相对本次长按起点的累计水平位移。
  void updateDisplacement(double horizontalDx);

  void end();
}
