import { del, get, patch, post, put } from './client'

export interface StrangerPet { id: number; user_id: string; pet_id: string; pet_name: string; nickname: string; power: number | null; dominant_type: number | null; raw_data: Record<string, unknown>; imported_at: string; updated_at: string; power_updated_at: string }
export interface InternalPet { id: number; user_id: string; pet_id: string; created_at: string; updated_at: string }

export interface AdminUser {
  id: number
  username: string
  role: 'user' | 'admin'
  status: 'active' | 'disabled'
  created_at: string
  updated_at: string
  coins: number
}

export interface PageResult<T> { items: T[]; total: number; page: number; page_size: number }
export interface AdminSettings { registration_enabled: boolean; register_gift_coins: number; system_name: string; mokant_host?: string; mokant_port?: number; mokant_token_set?: boolean }
export interface OfficialBotConfig { app_id: string; client_secret: string; client_secret_set: boolean; group_openid: string; offline_enabled: boolean; offline_template: string; activity_started_enabled: boolean; activity_started_template: string; activity_completed_enabled: boolean; activity_completed_template: string }
export interface AdminQQPlan { id: number; name: string; price: number; service_days: number; description: string; status: 'active' | 'inactive'; sort_order: number; created_at: string; updated_at: string }
export interface AuditLog { id: number; admin_user_id: number; admin_username: string; action: string; resource_type: string; resource_id: string; details: string; created_at: string }
export interface RedeemCodeItem { id: number; code: string; amount: number; status: 'unused' | 'redeemed'; created_by: number; created_by_name: string; redeemed_by: number | null; redeemed_by_name: string; created_at: string; redeemed_at: string }
export interface RedeemCodeStats { total: number; unused: number; redeemed: number }
export interface UserTrendItem { date: string; registered: number; total: number }
export interface SystemStatus { status: string; database: string; database_latency_ms: number; version: string; environment: string; memory_alloc_mb: number; database_size_mb: number; checked_at: string }
export interface Announcement { id: number; title: string; content: string; status: 'published' | 'archived'; created_by: string; created_at: string; updated_at: string }
export interface MokantStatus { state: string; connected: boolean; last_checked: string; last_error?: string }
export interface MokantNode { id: number; name: string; host: string; port: number; token_set: boolean; is_default: boolean; created_at: string; updated_at: string; status: MokantStatus }
export interface MokantNodeInput { name: string; host: string; port: number; token: string; is_default: boolean }
export interface MokantBot { device_profile_id: number; extra_info: string; friend_count: number; group_count: number; last_active: number; level: number; login_time: number; nickname: string; online_time: number; protocol_id: number; received: number; self_id: number; sent: number; status: 0 | 1 | 2; bound_user_id: number | null; bound_username: string | null; service_expires_at: string; node_id: number; node_name: string; framework_present: boolean; framework_checked: boolean; database_present: boolean }

export const getStrangerPets = (page: number, pageSize: number) => get<PageResult<StrangerPet>>(`/api/admin/stranger-pets?page=${page}&page_size=${pageSize}`)
export const getStrangerPet = (id: number) => get<StrangerPet>(`/api/admin/stranger-pets/${id}`)
export const deleteStrangerPet = (id: number) => del<null>(`/api/admin/stranger-pets/${id}`)
export const importStrangerPet = (data: Record<string, unknown>) => post<null>('/api/admin/stranger-pets/import', data)
export const importStrangerTXT = (data: string) => post<null>('/api/admin/stranger-pets/import-txt', data)
export const getInternalPets = () => get<InternalPet[]>('/api/admin/internal-pets')
export const createInternalPet = (data: { user_id: string; pet_id: string }) => post<InternalPet>('/api/admin/internal-pets', data)
export const deleteInternalPet = (id: number) => del<null>(`/api/admin/internal-pets/${id}`)

export const getUsers = (params: Record<string, string | number>) => get<PageResult<AdminUser>>(`/api/admin/users?${new URLSearchParams(Object.entries(params).map(([key, value]) => [key, String(value)]))}`)
export const updateUserStatus = (id: number, status: string) => patch<null>(`/api/admin/users/${id}/status`, { status })
export const updateUserRole = (id: number, role: string) => patch<null>(`/api/admin/users/${id}/role`, { role })
export const getSettings = () => get<AdminSettings>('/api/admin/settings')
export const updateSettings = (data: AdminSettings) => patch<AdminSettings>('/api/admin/settings', data)
export const getOfficialBotConfig = () => get<OfficialBotConfig>('/api/admin/official-bot')
export const updateOfficialBotConfig = (data: OfficialBotConfig) => put<OfficialBotConfig>('/api/admin/official-bot', data)
export const testOfficialBot = () => post<null>('/api/admin/official-bot/test')
export const getAuditLogs = (page: number, pageSize: number) => get<PageResult<AuditLog>>(`/api/admin/audit-logs?page=${page}&page_size=${pageSize}`)
export const getRedeemCodes = (page: number, pageSize: number, status = '') => get<PageResult<RedeemCodeItem>>(`/api/admin/redeem-codes?page=${page}&page_size=${pageSize}${status ? `&status=${status}` : ''}`)
export const getRedeemCodeStats = () => get<RedeemCodeStats>('/api/admin/redeem-codes/stats')
export const resetUserPassword = (id: number) => post<{ username: string; password: string }>(`/api/admin/users/${id}/password/reset`)
export const getUserTrend = (range: string) => get<{ range: string; items: UserTrendItem[] }>(`/api/admin/statistics/users?range=${range}`)
export const getSystemStatus = () => get<SystemStatus>('/api/admin/system/status')
export const getAdminAnnouncements = () => get<Announcement[]>('/api/admin/announcements')
export const getMokantNodes = () => get<MokantNode[]>('/api/admin/mokant/nodes')
export const createMokantNode = (data: MokantNodeInput) => post<{ id: number }>('/api/admin/mokant/nodes', data)
export const updateMokantNode = (id: number, data: MokantNodeInput) => put<null>(`/api/admin/mokant/nodes/${id}`, data)
export const deleteMokantNode = (id: number) => del<null>(`/api/admin/mokant/nodes/${id}`)
export const connectMokantNode = (id: number) => post<MokantStatus>(`/api/admin/mokant/nodes/${id}/connect`)
export const disconnectMokantNode = (id: number) => post<MokantStatus>(`/api/admin/mokant/nodes/${id}/disconnect`)
export const getMokantNodeStatus = (id: number) => get<MokantStatus>(`/api/admin/mokant/nodes/${id}/status`)
export const getMokantBots = () => get<MokantBot[]>('/api/admin/mokant/bots')
export const updateQQBinding = (qq: number, userId: number, planId: number) => patch<{ qq_number: string; user_id: number; username: string; service_expires_at: string; plan_id: number }>(`/api/admin/qq-accounts/${qq}/binding`, { user_id: userId, plan_id: planId })
export const deleteLocalQQ = (qq: number) => del<null>(`/api/admin/qq-accounts/${qq}/binding`)
export const saveAnnouncement = (data: { id?: number; title: string; content: string; status: string }) => data.id ? patch<null>(`/api/admin/announcements/${data.id}`, data) : post<null>('/api/admin/announcements', data)
export const getAdminQQPlans = () => get<AdminQQPlan[]>('/api/admin/qq-plans')
export const createAdminQQPlan = (data: Partial<AdminQQPlan>) => post<AdminQQPlan>('/api/admin/qq-plans', data)
export const updateAdminQQPlan = (id: number, data: Partial<AdminQQPlan>) => patch<null>(`/api/admin/qq-plans/${id}`, data)
export const deleteAdminQQPlan = (id: number) => del<null>(`/api/admin/qq-plans/${id}`)
