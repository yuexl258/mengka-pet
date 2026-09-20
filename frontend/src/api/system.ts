import { get } from './client'

export interface HealthData {
  status: string
  database: string
}

export interface VersionData {
  name: string
  version: string
  environment: string
}

export interface SystemSettings { system_name: string }

export const getHealth = () => get<HealthData>('/healthz')
export const getVersion = () => get<VersionData>('/api/version')
export const getSystemSettings = () => get<SystemSettings>('/api/system/settings')
