export interface APIKeyItem {
  id: number
  name: string
  key_prefix: string
  created_by: number
  last_used_at: string | null
  created_at: string
}

export interface CreateAPIKeyResponse extends APIKeyItem {
  api_key: string
}
