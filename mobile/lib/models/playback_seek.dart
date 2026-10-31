/// 播放进度 seek 共享契约：一键跳过步长、末尾余量、基线窗口与水平拖动换算。
///
/// 本文件为纯 Dart，不依赖播放器、Provider 或 Flutter，供屏幕层与 Notifier 共用。
library;

/// 一键跳过固定步长（OP/ED 约 1.5 分钟）。
const Duration skipForwardStep = Duration(seconds: 80);

/// 相对 seek 的上限距片尾的余量：目标最大为 duration - seekEndMargin。
const Duration seekEndMargin = Duration(seconds: 1);

/// 连续 seek 基线的有效窗口。
const Duration seekBaselineWindow = Duration(milliseconds: 1500);

/// 水平拖动调进度的固定速度：每逻辑像素对应秒数（与视频时长、屏宽无关）。
const double horizontalSeekSecondsPerLogicalPixel = 0.3;

/// 水平累计位移 dx（逻辑像素，右正左负，相对 pan 起点）映射为进度增量。
/// 四舍五入到整秒；非有限值返回 Duration.zero；不做越界钳制（调用方钳制）。
Duration horizontalDragSeekDelta(double dx) {
  if (!dx.isFinite) {
    return Duration.zero;
  }
  final seconds = dx * horizontalSeekSecondsPerLogicalPixel;
  return Duration(seconds: seconds.round());
}
