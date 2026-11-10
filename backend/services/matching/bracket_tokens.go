package matching

// extractEpisodeTokens 预抽取括号集号、季+集组合、总集数与裸文件名集号。
// Stage 0 为恒等桩：返回原串、不写 extras。
func extractEpisodeTokens(stem string, ex *fileExtras) string {
	return stem
}
