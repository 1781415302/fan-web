package matching

// preExtractDir 清洗目录串并识别目录季号。Stage 0 为恒等桩：返回原串、季号 0。
func preExtractDir(dir string) (cleaned string, season int) {
	return dir, 0
}

// applyDirSeasonFallback 在标题取自目录且文件无季号时应用 dirSeason。
// Stage 0 为恒等桩：不改 ev。
func applyDirSeasonFallback(ev *FileEvidence, dirSeason int) {
}
