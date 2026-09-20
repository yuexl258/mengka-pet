import { get, post } from './client'

export interface User {
  id: number
  username: string
  role: string
  status: string
  created_at: string
}

export const getMe = () => get<User>('/api/auth/me')
export const getSession = () => get<User | null>('/api/auth/session')
export const login = (username: string, password: string) => post<User>('/api/auth/login', { username, password })
export const register = (username: string, password: string) => post<null>('/api/auth/register', { username, password })
export const logout = () => post<null>('/api/auth/logout')
export const changePassword = (data: { current_password: string; new_password: string; confirm_password: string }) => post<null>('/api/auth/password/change', data)
