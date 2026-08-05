export interface UnidentifiedFile {
  file_name: string
  reason: string
}

export interface LibraryScanResult {
  total_files: number
  skipped: number
  new_animes: number
  new_episodes: number
  unidentified: UnidentifiedFile[]
}
