package database

// IsFileAssociated 判断给定文件是否已被关联为某番剧的剧集。
// fileName 为文件名（episodes.file_path 存储的正是文件名），dirPath 为文件所在相对目录
// （animes.file_path 存储的正是相对目录）。两端路径统一把反斜杠替换为斜杠后再比较，
// 以兼容 Windows（\）与类 Unix（/）分隔符不一致导致的漏判（ListAnimesByFilePath 已
// 提示同类分隔符敏感性）。当前数据形态为相对文件名+相对目录，故按此精确匹配；若将来
// 改为全路径存储，需调整为按 episode id 或规范化全路径关联。
func IsFileAssociated(fileName, dirPath string) (bool, error) {
	var count int
	err := DB.QueryRow(
		"SELECT COUNT(*) "+
			"FROM episodes ep "+
			"JOIN animes a ON ep.anime_id = a.id "+
			"WHERE REPLACE(ep.file_path, '\\', '/') = REPLACE(?, '\\', '/') "+
			"  AND REPLACE(a.file_path, '\\', '/') = REPLACE(?, '\\', '/')",
		fileName, dirPath,
	).Scan(&count)
	return count > 0, err
}
