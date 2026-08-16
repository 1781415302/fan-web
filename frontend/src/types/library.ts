export interface MatchCandidate {
  id: number
  name: string
  name_cn: string
  score: number
}

export interface UnidentifiedFile {
  file_name: string
  reason: string
  file_path: string
  candidates: MatchCandidate[]
}

export interface LibraryScanResult {
  total_files: number
  skipped: number
  new_animes: number
  new_episodes: number
  unidentified: UnidentifiedFile[]
}
