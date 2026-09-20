import { get, patch, post } from './client'

export interface QQNode {
  id: number
  name: string
  enabled: boolean
  proxy_enabled: boolean
  proxy_type: string
  remark: string
  last_check: string
  account_count: number
  online_account_count: number
}
export interface UserQQAccount {
  qq_number: string
  created_at: string
  updated_at: string
  nickname: string
  status: 0 | 1 | 2
  protocol_id?: number
  device_profile_id?: number
  service_months: number
  service_expires_at: string
  expired: boolean
  node_id: number
  node_name: string
}

export interface QQAccountOption { id: number; name: string }
export interface QQAccountOptions { protocols: QQAccountOption[]; device_profiles: QQAccountOption[] }
export interface QQAccountPrice { monthly_price: number }
export const getUserQQAccounts = () => get<UserQQAccount[]>('/api/qq/account')
export const getQQNodes = () => get<QQNode[]>('/api/qq/account/nodes')
export const getQQAccountOptions = () => get<QQAccountOptions>('/api/qq/account/options')
export const getQQAccountPrice = () => get<QQAccountPrice>('/api/qq/account/price')
export interface QQPlan { id: number; name: string; price: number; service_days: number; description: string }
export const getQQPlans = () => get<QQPlan[]>('/api/qq/plans')
export const getUserQQAccountInfo = (qq: string) => get<Record<string, unknown>>(`/api/qq/account/${encodeURIComponent(qq)}/info`)
export const addQQ = (data: { qq_number: string; password: string; protocol_id: number; device_profile_id: number; service_months: number; plan_id?: number; node_id: number }) => post<{ qq_number: string; result: unknown }>('/api/qq/account/add', data)
export const updateQQ = (qq: string, data: { password: string; protocol_id: number; device_profile_id: number; node_id: number }) => patch<unknown>(`/api/qq/account/${encodeURIComponent(qq)}`, data)
export interface QQLoginResult { code: number; message: string; slider_url?: string; identity_url?: string; security_url?: string; security_verify?: Record<string, unknown> }
export const loginQQ = (qq: string) => post<QQLoginResult>(`/api/qq/account/${encodeURIComponent(qq)}/login`)
export const getQQSecurityMethods = (qq: string) => get<Record<string, unknown>>(`/api/qq/account/${encodeURIComponent(qq)}/security-methods`)
export const createQQSecurityQR = (qq: string) => post<Record<string, unknown>>(`/api/qq/account/${encodeURIComponent(qq)}/security-qr`)
export const queryQQSecurityQRStatus = (qq: string, guarantee_token: string) => post<Record<string, unknown>>(`/api/qq/account/${encodeURIComponent(qq)}/security-qr/status`, { guarantee_token })
export const confirmQQSecurity = (qq: string) => post<QQLoginResult>(`/api/qq/account/${encodeURIComponent(qq)}/security-confirm`)
export const getQQSecuritySMS = (qq: string, verify_type: number, sign: string) => post<Record<string, unknown>>(`/api/qq/account/${encodeURIComponent(qq)}/security-sms`, { verify_type, sign })
export const checkQQSecuritySMS = (qq: string, verify_type: number, sign: string, code?: string) => post<Record<string, unknown>>(`/api/qq/account/${encodeURIComponent(qq)}/security-sms/check`, { verify_type, sign, code })
export const submitQQSecuritySlider = (qq: string, ticket: string, randstr: string) => post<QQLoginResult>(`/api/qq/account/${encodeURIComponent(qq)}/security-slider`, { ticket, randstr })
export const submitQQIdentityCaptcha = (qq: string, ticket: string, randstr: string) => post<Record<string, unknown>>(`/api/qq/account/${encodeURIComponent(qq)}/identity-captcha`, { ticket, randstr })
export const submitQQIdentityPhone = (qq: string, mobile: string, area_code: string) => post<Record<string, unknown>>(`/api/qq/account/${encodeURIComponent(qq)}/identity-phone`, { mobile, area_code })
export const confirmQQIdentitySMS = (qq: string, mobile: string, area_code: string) => post<QQLoginResult>(`/api/qq/account/${encodeURIComponent(qq)}/identity-sms/confirm`, { mobile, area_code })
export const offlineQQ = (qq: string) => post<unknown>(`/api/qq/account/${encodeURIComponent(qq)}/offline`)
export const renewQQ = (qq: string, service_months: number) => post<{ qq_number: string; service_expires_at: string; service_days: number; total_coins: number }>(`/api/qq/account/${encodeURIComponent(qq)}/renew`, { service_months })
export const renewQQPlan = (qq: string, plan_id: number) => post<{ qq_number: string; service_expires_at: string; service_days: number; total_coins: number }>(`/api/qq/account/${encodeURIComponent(qq)}/renew`, { plan_id })
