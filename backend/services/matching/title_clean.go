package matching

// extractFansubMeta 预抽取 CJK 字幕组/联署/元数据块。
// Stage 0 为恒等桩：返回原串、不写 extras。
func extractFansubMeta(stem string, ex *fileExtras) string {
	return stem
}

// extractOrdinalWords 预抽取 Part N、Second/Third/Final Season 等序数词。
// Stage 0 为恒等桩：返回原串、不写 extras。
func extractOrdinalWords(stem string, ex *fileExtras) string {
	return stem
}
