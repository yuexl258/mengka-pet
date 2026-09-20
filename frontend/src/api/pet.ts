import { get, post, put } from './client'

export interface PetProfile {
  pet_name?: string
  pet_id?: string | number
  user_id?: string
  avatar_url?: string
  personality?: string
  species?: string
  job_name?: string
  traits?: string[]
  [key: string]: unknown
}

export interface PetVitals {
  mood?: number
  hunger?: number
  cleanliness?: number
  total?: number
  gold?: number
  [key: string]: unknown
}

export interface PetFoodCatalog {
  [key: string]: unknown
}

export interface PetAttribute {
  group?: number
  name?: string
  value?: number
  [key: string]: unknown
}

export interface PetAttributes {
  pet_id?: string | number
  attributes?: PetAttribute[]
  [key: string]: unknown
}

export interface PetMetricChanges {
  gold: number
  attributes: Record<string, number>
}

export interface PetDailyStat {
  date: string
  attributes: Record<string, number>
  gold: number | null
  power: number | null
}

export interface PetFatigueStatus {
  todayHours: number
  schoolHours: number
  workHours: number
}

export interface PetMedal {
  medal_id?: string | number
  id?: string | number
  name?: string
  medal_name?: string
  progress?: string | number
  category?: string
  image_url?: string
  icon_url?: string
  requirement?: string
  description?: string
  [key: string]: unknown
}

export interface PetMedalGalleryItem extends PetMedal {
  acquired?: boolean
  equipped?: boolean
  medal?: PetMedal
}

export interface PetMedalGallery {
  pet_id?: string | number
  medals: PetMedalGalleryItem[]
}

export interface PetInteractionSegment {
  text: string
  font_weight?: string
}

export interface PetInteractionMessage {
  user_id: string
  segments: PetInteractionSegment[]
  timestamp: number
  message_id: string
  pet_name: string
  event_type: number
}

export interface PetInteractionMessages {
  limit: number
  messages: PetInteractionMessage[]
}

export interface PetPKPower {
  dominant_type: number
  power: number
}

export interface PetPKStartResult {
  success: boolean
  pet_id: string
  friend_id: string
  friend_pet_id: string
  friend_pet_name: string
  story_id: string
  started: boolean
}

export interface PetPKStatusResult {
  success: boolean
  pet_id: string
  story_id: string
  status_received: boolean
  response_empty: boolean
  [key: string]: unknown
}

export interface AutoPKConfig {
  enabled: boolean
  target_starts: number
  start_time: string
}

export interface AutoPKState {
  running: boolean
  successful_starts: number
  today_starts: number
  target_starts: number
  start_time: string
  last_opponent_user_id?: string
  message: string
  last_error?: string
  last_run_at?: string
}

export interface AutoPKLog {
  id: number
  action: string
  opponent_user_id?: string
  opponent_pet_id?: string
  opponent_power: number
  success: boolean
  message: string
  details?: string
  created_at: string
}

export interface PetPKStranger {
  pet_id: string
  pet_name: string
  user_id: string
  nickname: string
  dominant_type: number
  power: number
  pet_resolved: boolean
  [key: string]: unknown
}

export interface PetProfileItem {
  qq_number: string
  qq_nickname: string
  profile: PetProfile
  vitals: PetVitals | null
  attributes: PetAttributes | null
  metric_changes: PetMetricChanges
  power: number | null
  baseline: number | null
  dominant_type: number | null
  power_change: number
  fatigue: PetFatigueStatus | null
}

export interface PetRankingItem {
  power: number
  updated_at: string
  pet_name: string
  qq_number: string
  dominant_type: number | null
}

export type PetActivityType = 'school' | 'work' | 'adventure'

export interface PetActivityOption {
  name?: string
  icon_url?: string
  cost?: string
  duration?: string
  duration_seconds?: number
  sub_event_type?: number
  reward?: string
  description?: string
  can_do?: boolean
  unavailable_reason?: string
  [key: string]: unknown
}

export interface PetActivityCareer {
  career_type?: number
  name?: string
  status_code?: number
  message?: string
  [key: string]: unknown
}

export interface PetActivityOverview {
  activity?: PetActivityType
  current_stage?: number
  current_career_type?: number
  last_sub_event_type?: number
  entries?: PetActivityCareer[]
  attributes?: unknown[]
  [key: string]: unknown
}

export interface PetActivityOptionsResult {
  activity?: PetActivityType
  career_name?: string
  options?: PetActivityOption[]
  overview?: PetActivityOverview | null
  current_career_type?: number
  [key: string]: unknown
}

export interface PetActivityStartResult {
  success?: boolean
  pet_id?: string
  activity?: PetActivityType
  option_name?: string
  story_id?: string
  started?: boolean
  [key: string]: unknown
}

export interface PetActivityStatus {
  story_id?: string
  state_code?: number
  remaining_seconds?: number
  duration_seconds?: number
  started_at?: number
  recallable?: boolean
  [key: string]: unknown
}

export interface PetAutoControlPlanItem {
  id: string
  activity: PetActivityType
  control_mode: 'count' | 'duration'
  count: number
  hours: number
  learning_target?: string
  work_career_type?: number
  work_option?: string
  work_sub_event_type?: number
  executed_count: number
  executed_seconds: number
  completed: boolean
}

export interface PetAutoControlConfig {
  enabled: boolean
  plan: PetAutoControlPlanItem[]
  learning_target: string
  activity_order: PetActivityType[]
  school_count: number
  work_count: number
  adventure_count: number
  school_remaining: number
  work_remaining: number
  adventure_remaining: number
  school_executed: number
  work_executed: number
  adventure_executed: number
  school_control_mode: 'count' | 'duration'
  work_control_mode: 'count' | 'duration'
  adventure_control_mode: 'count' | 'duration'
  school_duration_seconds: number
  work_duration_seconds: number
  adventure_duration_seconds: number
  school_duration_executed: number
  work_duration_executed: number
  adventure_duration_executed: number
  work_option: string
  auto_feed_enabled: boolean
  auto_feed_threshold: number
  auto_feed_food: string
  auto_bathe_enabled: boolean
  auto_bathe_threshold: number
  auto_bathe_item: string
  encourage_probability: number
  auto_poke_enabled: boolean
  auto_poke_start_time: string
  auto_poke_daily_count: number
  auto_poke_interval_seconds: number
  auto_poke_last_run_at?: string
  auto_poke_last_friend_qq?: string
  last_reset_date: string
}

export interface PetAutoControlInventory {
  food_inventory: PetFoodCatalog | PetFoodCatalog[]
  bath_inventory: PetFoodCatalog | PetFoodCatalog[]
  bath_catalog: PetFoodCatalog | PetFoodCatalog[]
}

export interface PetAutoControlState {
  running: boolean
  current_activity: PetActivityType | ''
  current_option: string
  message: string
  last_action: string
  last_error?: string
  last_run_at?: string
}

export interface PetAutoControlLog {
  id: number
  action: string
  activity: PetActivityType | ''
  option_name: string
  success: boolean
  message: string
  details: string
  created_at: string
}

export interface PetAccount { qq_number: string; nickname: string; service_months: number; service_expires_at: string; expired: boolean; online: boolean }

export interface PetFriend {
  friend_qq: string
  nickname: string
  has_pet: boolean
  pet_id: string
  friend_data?: Record<string, unknown>
  pet_data?: Record<string, unknown>
  updated_at?: string
}

export interface FriendPetProfile {
  friend_uin: string
  pet_id: string
  source?: string
  partial?: boolean
  vitals?: {
    cleanliness?: number
    hunger?: number
    mood?: number
    total?: number
    pet_id?: string
  }
  [key: string]: unknown
}

export interface PetFriendRefreshState {
  status: 'idle' | 'running' | 'completed' | 'failed'
  message: string
  error?: string
  total_count?: number
  started_at?: string | null
  finished_at?: string | null
  updated_at?: string
}

export const getPetFriends = (qq: string) => get<PetFriend[]>(`/api/pets/${encodeURIComponent(qq)}/friends`)
export const refreshPetFriends = (qq: string) => post<PetFriendRefreshState>(`/api/pets/${encodeURIComponent(qq)}/friends/refresh`)
export const filterPetFriends = (qq: string) => post<PetFriendRefreshState>(`/api/pets/${encodeURIComponent(qq)}/friends/filter`)
export const getPetFriendRefreshState = (qq: string) => get<PetFriendRefreshState>(`/api/pets/${encodeURIComponent(qq)}/friends/refresh-state`)
export const bindPetFriendID = (qq: string, friendID: string, petID: string) => put<FriendPetProfile>(`/api/pets/${encodeURIComponent(qq)}/friends/${encodeURIComponent(friendID)}/pet-id`, { pet_id: petID })
export const getPetFriendProfile = (qq: string, friendID: string) => get<FriendPetProfile>(`/api/pets/${encodeURIComponent(qq)}/friends/${encodeURIComponent(friendID)}/pet-profile`)
export const pokePetFriend = (qq: string, friendID: string) => post<unknown>(`/api/pets/${encodeURIComponent(qq)}/friends/${encodeURIComponent(friendID)}/poke`)
export const getPetAccounts = () => get<PetAccount[]>('/api/pets/accounts')
export const getPetProfile = (qq: string) => get<PetProfileItem>(`/api/pets/${encodeURIComponent(qq)}`)
export const getPetPKStrangers = (qq: string) => get<PetPKStranger[]>(`/api/pets/${encodeURIComponent(qq)}/pk-strangers`)
export const getPetPKCache = () => get<PetPKStranger[]>('/api/pets/pk-cache')
export const getPetPKPower = (qq: string, petID: string) => get<PetPKPower>(`/api/pets/${encodeURIComponent(qq)}/pk-power?pet_id=${encodeURIComponent(petID)}`)
export const startPetPK = (qq: string, friendID: string, friendPetID: string) => post<PetPKStartResult>(`/api/pets/${encodeURIComponent(qq)}/pk-start`, { friend_id: friendID, friend_pet_id: friendPetID })
export const getPetPKStatus = (qq: string, storyID: string) => get<PetPKStatusResult>(`/api/pets/${encodeURIComponent(qq)}/pk-status?story_id=${encodeURIComponent(storyID)}`)
export const settlePetPK = (qq: string, storyID: string) => post<PetPKStatusResult>(`/api/pets/${encodeURIComponent(qq)}/pk-settle`, { story_id: storyID })
export const getAutoPKConfig = (qq: string) => get<AutoPKConfig>(`/api/pets/${encodeURIComponent(qq)}/auto-pk/config`)
export const updateAutoPKConfig = (qq: string, targetStarts: number, startTime = '00:00') => put<AutoPKConfig>(`/api/pets/${encodeURIComponent(qq)}/auto-pk/config`, { target_starts: targetStarts, start_time: startTime })
export const startAutoPK = (qq: string) => post<AutoPKState>(`/api/pets/${encodeURIComponent(qq)}/auto-pk/start`)
export const stopAutoPK = (qq: string) => post<AutoPKState>(`/api/pets/${encodeURIComponent(qq)}/auto-pk/stop`)
export const getAutoPKState = (qq: string) => get<AutoPKState>(`/api/pets/${encodeURIComponent(qq)}/auto-pk/state`)
export const getAutoPKLogs = (qq: string, limit = 20) => get<AutoPKLog[]>(`/api/pets/${encodeURIComponent(qq)}/auto-pk/logs?limit=${limit}`)
export const getPetMedalGallery = (qq: string) => get<PetMedalGallery>(`/api/pets/${encodeURIComponent(qq)}/medals`)
export const getPetInteractionMessages = (qq: string) => get<PetInteractionMessages>(`/api/pets/${encodeURIComponent(qq)}/interaction-messages`)
export const getPetDailyStats = (qq: string, range: '7d' | '30d' = '7d') => get<PetDailyStat[]>(`/api/pets/${encodeURIComponent(qq)}/daily-stats?range=${range}`)
export const feedPet = (qq: string, foodName: string) => post<unknown>(`/api/pets/${encodeURIComponent(qq)}/feed?food_name=${encodeURIComponent(foodName)}`)
export const buyPetFood = (qq: string) => post<unknown>(`/api/pets/${encodeURIComponent(qq)}/food-purchase`)
export const getRankings = () => get<PetRankingItem[]>('/api/rankings')
export const getPetActivityOverview = (qq: string, activity: PetActivityType) => get<PetActivityOverview>(`/api/pets/${encodeURIComponent(qq)}/activity-overview?activity=${encodeURIComponent(activity)}`)
export const getPetActivityOptions = (qq: string, activity: PetActivityType, careerType?: number) => get<PetActivityOptionsResult>(`/api/pets/${encodeURIComponent(qq)}/activity-options?activity=${encodeURIComponent(activity)}${careerType !== undefined ? `&career_type=${encodeURIComponent(careerType)}` : ''}`)
export const startPetActivity = (qq: string, activity: PetActivityType, optionName: string, subEventType?: number) => post<PetActivityStartResult>(`/api/pets/${encodeURIComponent(qq)}/activity-start`, { activity, option_name: optionName, ...(subEventType !== undefined ? { sub_event_type: subEventType } : {}) })
export const getPetActivityStatus = (qq: string) => get<PetActivityStatus>(`/api/pets/${encodeURIComponent(qq)}/activity-status`)
export const encouragePetActivity = (qq: string) => post<unknown>(`/api/pets/${encodeURIComponent(qq)}/activity-encourage`)
export const getPetAutoControlInventory = (qq: string) => get<PetAutoControlInventory>(`/api/pets/${encodeURIComponent(qq)}/auto-control/inventory`)
export const getPetAutoControlConfig = (qq: string) => get<PetAutoControlConfig>(`/api/pets/${encodeURIComponent(qq)}/auto-control/config`)
export const savePetAutoControlConfig = (qq: string, config: PetAutoControlConfig) => put<PetAutoControlConfig>(`/api/pets/${encodeURIComponent(qq)}/auto-control/config`, config)
export const getPetAutoControlState = (qq: string) => get<PetAutoControlState>(`/api/pets/${encodeURIComponent(qq)}/auto-control/state`)
export const getPetAutoControlLogs = (qq: string, limit = 20) => get<PetAutoControlLog[]>(`/api/pets/${encodeURIComponent(qq)}/auto-control/logs?limit=${limit}`)
export const bathePet = (qq: string) => post<unknown>(`/api/pets/${encodeURIComponent(qq)}/bathe?item_name=${encodeURIComponent('香皂片')}`)
export const buyPetBathItem = (qq: string, itemName: string) => post<unknown>(`/api/pets/${encodeURIComponent(qq)}/bath-purchase?item_name=${encodeURIComponent(itemName)}`)
