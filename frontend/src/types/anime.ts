export interface Anime {
  id: number
  title: string
  title_cn: string
  bangumi_id: number
  cover: string
  summary: string
  ep_count: number
  file_path: string
  created_at: string
  watched_count?: number
}

export interface Episode {
  id: number
  anime_id: number
  ep_number: number
  title: string
  file_path: string
  duration: number
}

export interface PaginatedAnimes {
  items: Anime[]
  total: number
  page: number
  page_size: number
}

export interface BangumiSearchItem {
  id: number
  name: string
  name_cn: string
  summary: string
  eps_count: number
  cover: string
}

export interface ScanResult {
  scanned: number
  episodes: Episode[]
}
