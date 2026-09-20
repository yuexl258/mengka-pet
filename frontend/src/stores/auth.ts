import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { getSession, login as loginRequest, logout as logoutRequest, register as registerRequest, type User } from '../api/auth'
import { usePetPKStore } from './petPK'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const initialized = ref(false)
  const isAuthenticated = computed(() => user.value !== null)

  async function loadMe() {
    try {
      user.value = (await getSession()).data
    } catch {
      user.value = null
    } finally {
      initialized.value = true
    }
  }

  async function login(username: string, password: string) { user.value = (await loginRequest(username, password)).data }
  async function register(username: string, password: string) { await registerRequest(username, password) }
  async function logout() { try { await logoutRequest() } finally { user.value = null; usePetPKStore().clear() } }

  return { user, initialized, isAuthenticated, loadMe, login, register, logout }
})
