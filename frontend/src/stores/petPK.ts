import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { getPetPKCache, getPetPKPower, getPetPKStrangers, type PetPKStranger } from '../api/pet'

export type CachedPetPKStranger = PetPKStranger & {
  imported_at?: string
  updated_at?: string
  power_updated_at?: string | null
}

export const usePetPKStore = defineStore('pet-pk', () => {
  const itemsByQQ = ref<Record<string, CachedPetPKStranger[]>>({})
  const allStrangers = ref<CachedPetPKStranger[]>([])
  const loadingQQ = ref('')
  const updatingUserID = ref('')

  async function load(qq: string) {
    if (!qq) return
    loadingQQ.value = qq
    try {
      const result = await getPetPKStrangers(qq)
      itemsByQQ.value[qq] = (result.data || []) as CachedPetPKStranger[]
    } finally {
      if (loadingQQ.value === qq) loadingQQ.value = ''
    }
  }

  async function loadAll() {
    const result = await getPetPKCache()
    allStrangers.value = (result.data || []) as CachedPetPKStranger[]
  }

  async function updatePower(qq: string, userID: string) {
    const item = itemsByQQ.value[qq]?.find(value => value.user_id === userID)
    if (!qq || !item?.pet_id) return
    updatingUserID.value = userID
    try {
      const result = await getPetPKPower(qq, String(item.pet_id))
      if (!result.data) return
      item.dominant_type = Number(result.data.dominant_type)
      item.power = Number(result.data.power)
      item.power_updated_at = new Date().toISOString()
      item.updated_at = item.power_updated_at
    } finally {
      if (updatingUserID.value === userID) updatingUserID.value = ''
    }
  }

  function itemsForQQ(qq: string) { return itemsByQQ.value[qq] || [] }
  function clear() { itemsByQQ.value = {}; allStrangers.value = []; loadingQQ.value = ''; updatingUserID.value = '' }

  return { itemsByQQ, loadingQQ, updatingUserID, allStrangers: computed(() => allStrangers.value), itemsForQQ, load, loadAll, updatePower, clear }
})
