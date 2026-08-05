package database

func IsFileAssociated(fileName, dirPath string) (bool, error) {
	var count int
	err := DB.QueryRow(
		`SELECT COUNT(*)
		 FROM episodes ep
		 JOIN animes a ON ep.anime_id = a.id
		 WHERE ep.file_path = ? AND a.file_path = ?`,
		fileName, dirPath,
	).Scan(&count)
	return count > 0, err
}
