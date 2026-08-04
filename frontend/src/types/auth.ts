export interface User {
  id: number
  username: string
  is_admin: boolean
  created_at: string
}

export interface LoginData {
  token: string
  expires_at: string
  user: User
}
