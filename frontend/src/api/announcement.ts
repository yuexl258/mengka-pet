import { get } from './client'

export interface PublicAnnouncement { id: number; title: string; content: string; status: string; created_at: string; updated_at: string }
export const getAnnouncements = () => get<PublicAnnouncement[]>('/api/announcements')
