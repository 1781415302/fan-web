export interface EpisodeProgress {
  position: number
  watched: boolean
  updated_at: string
}

export interface AnimeProgress {
  episode_id: number
  position: number
  watched: boolean
  updated_at: string
}

export interface AnimeWithProgress {
  id: number
  title: string
  title_cn: string
  bangumi_id: number
  cover: string
  summary: string
  ep_count: number
  file_path: string
  created_at: string
  watched_count: number
}
