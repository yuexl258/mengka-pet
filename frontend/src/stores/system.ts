import { ref } from 'vue'
import { defineStore } from 'pinia'
import { getSystemSettings } from '../api/system'

export const useSystemStore = defineStore('system', () => {
  const name = ref('QQ 宠物')
  const loaded = ref(false)

  async function load() {
    if (loaded.value) return
    try {
      const result = await getSystemSettings()
      if (result.data?.system_name) name.value = result.data.system_name
    } catch {
      name.value = 'QQ 宠物'
    } finally {
      loaded.value = true
    }
  }

  return { name, loaded, load }
})
