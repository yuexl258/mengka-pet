<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useRoute } from 'vue-router'
import { bindPetFriendID, filterPetFriends, getPetAccounts, getPetFriendProfile, getPetFriendRefreshState, getPetFriends, pokePetFriend, refreshPetFriends, type FriendPetProfile, type PetAccount, type PetFriend, type PetFriendRefreshState } from '../../../api/pet'

const route = useRoute()
const loading = ref(true)
const friendsLoaded = ref(false)
const refreshing = ref(false)
const filtering = ref(false)
const accounts = ref<PetAccount[]>([])
const activeQQ = ref(String(route.query.qq || ''))
const friends = ref<PetFriend[]>([])
const refreshState = ref<PetFriendRefreshState | null>(null)
const pokingQQ = ref('')
const bindingVisible = ref(false)
const bindingFriend = ref<PetFriend | null>(null)
const petID = ref('')
const binding = ref(false)
const profileVisible = ref(false)
const profileLoading = ref(false)
const profileFriend = ref<PetFriend | null>(null)
const profile = ref<FriendPetProfile | null>(null)
let pollTimer: ReturnType<typeof setInterval> | null = null

const totalFriendCount = computed(() => refreshState.value?.total_count ?? friends.value.length)
const petFriends = computed(() => friends.value.filter(item => item.has_pet && !item.friend_qq.startsWith('2854')))
const petFriendCount = computed(() => petFriends.value.length)
const boundFriendCount = computed(() => petFriends.value.filter(item => item.pet_id).length)
const isRunning = computed(() => refreshing.value || filtering.value || refreshState.value?.status === 'running')

function qqAvatarUrl(qq: string) {
  return `https://q1.qlogo.cn/g?b=qq&nk=${encodeURIComponent(qq)}&s=100`
}

function stopPolling() {
  if (pollTimer) clearInterval(pollTimer)
  pollTimer = null
}

async function loadCache() {
  if (!activeQQ.value) return
  const [friendsResult, stateResult] = await Promise.all([getPetFriends(activeQQ.value), getPetFriendRefreshState(activeQQ.value)])
  friends.value = friendsResult.data || []
  refreshState.value = stateResult.data
  friendsLoaded.value = true
}

async function checkRefreshState() {
  if (!activeQQ.value) return
  try {
    const result = await getPetFriendRefreshState(activeQQ.value)
    refreshState.value = result.data
    if (result.data?.status === 'running') return
    stopPolling()
    if (result.data?.status === 'completed') {
      await loadCache()
      ElMessage.success(result.data.message || '好友列表刷新完成')
    } else if (result.data?.status === 'failed') {
      ElMessage.error(result.data.error || result.data.message || '好友列表刷新失败')
    }
  } catch (error) {
    stopPolling()
    refreshState.value = { status: 'failed', message: '好友刷新状态获取失败，请重新点击刷新', error: error instanceof Error ? error.message : '好友刷新状态获取失败' }
  }
}

function startPolling() {
  stopPolling()
  pollTimer = setInterval(() => void checkRefreshState(), 1500)
}

async function pokeFriend(friend: PetFriend) {
  if (!activeQQ.value || pokingQQ.value) return
  pokingQQ.value = friend.friend_qq
  try {
    await pokePetFriend(activeQQ.value, friend.friend_qq)
    ElMessage.success(`已踩一踩 ${friend.nickname || friend.friend_qq}`)
  } catch (error) {
    const message = error instanceof Error ? error.message : '踩一踩失败'
    if (message.includes('errorCode=136201') || message.includes('没有宠物')) {
      friends.value = friends.value.map(item => item.friend_qq === friend.friend_qq ? { ...item, has_pet: false } : item)
    }
    ElMessage.error(message)
  } finally {
    pokingQQ.value = ''
  }
}

function openBinding(friend: PetFriend) {
  bindingFriend.value = friend
  petID.value = ''
  bindingVisible.value = true
}

async function bindFriend() {
  const friend = bindingFriend.value
  const value = petID.value.trim()
  if (!activeQQ.value || !friend || !value || binding.value) return
  binding.value = true
  try {
    await bindPetFriendID(activeQQ.value, friend.friend_qq, value)
    ElMessage.success('好友 Pet ID 绑定成功')
    bindingVisible.value = false
    bindingFriend.value = null
    await loadCache()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '好友 Pet ID 绑定失败')
  } finally {
    binding.value = false
  }
}

async function openProfile(friend: PetFriend) {
  if (!activeQQ.value || profileLoading.value) return
  profileVisible.value = true
  profileLoading.value = true
  profileFriend.value = friend
  profile.value = null
  try {
    const result = await getPetFriendProfile(activeQQ.value, friend.friend_qq)
    profile.value = result.data
  } catch (error) {
    profileVisible.value = false
    ElMessage.error(error instanceof Error ? error.message : '好友宠物资料加载失败')
  } finally {
    profileLoading.value = false
  }
}

async function refresh() {
  if (!activeQQ.value || isRunning.value) return
  refreshing.value = true
  try {
    const result = await refreshPetFriends(activeQQ.value)
    refreshState.value = result.data || { status: 'running', message: '正在刷新好友' }
    ElMessage.info('数据刷新中，请勿频繁点击')
    startPolling()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '好友列表刷新失败')
  } finally {
    refreshing.value = false
  }
}

async function filterFriends() {
  if (!activeQQ.value || isRunning.value) return
  filtering.value = true
  try {
    const result = await filterPetFriends(activeQQ.value)
    refreshState.value = result.data || { status: 'running', message: '正在过滤好友' }
    ElMessage.info('正在复查未绑定 Pet ID 的宠物好友')
    startPolling()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '好友过滤失败')
  } finally {
    filtering.value = false
  }
}

function selectAccount(account: PetAccount) {
  stopPolling()
  activeQQ.value = account.qq_number
  friends.value = []
  refreshState.value = null
  friendsLoaded.value = false
  void loadCache().catch(error => ElMessage.error(error instanceof Error ? error.message : '好友缓存加载失败'))
}

function formatTime(value?: string) {
  if (!value) return '暂无'
  return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(value))
}

function profileValue(value: unknown) {
  return value === undefined || value === null || value === '' ? '暂无' : String(value)
}

async function load() {
  loading.value = true
  try {
    const result = await getPetAccounts()
    accounts.value = result.data || []
    if (!activeQQ.value) activeQQ.value = accounts.value.find(item => !item.expired)?.qq_number || ''
    if (activeQQ.value) {
      await loadCache()
      if (refreshState.value?.status === 'running') startPolling()
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '好友数据加载失败')
  } finally {
    loading.value = false
  }
}

onMounted(() => void load())
onUnmounted(stopPolling)
</script>

<template>
  <div class="page-heading"><div><h1>好友管理</h1><p>管理好友 Pet ID、查看宠物资料并进行好友互动。</p></div></div>
  <el-card v-loading="loading" class="pet-friends-card">
    <el-empty v-if="!accounts.length && !loading" description="暂无已绑定的 QQ 账号" />
    <template v-else>
      <div class="pet-account-switcher"><button v-for="account in accounts" :key="account.qq_number" type="button" class="pet-account-button" :class="{ 'is-active': activeQQ === account.qq_number }" @click="selectAccount(account)"><span class="pet-account-button-name">{{ account.nickname || account.qq_number }}</span><span class="pet-account-button-qq">QQ {{ account.qq_number }}</span></button></div>
      <div class="pet-friends-toolbar"><div class="pet-friends-summary"><span>当前好友 <strong>{{ friends.length }}/{{ totalFriendCount }}</strong></span><span>拥有宠物 <strong>{{ petFriendCount }}</strong></span><span>已绑定 Pet ID <strong>{{ boundFriendCount }}</strong></span></div><div class="pet-friends-toolbar-actions"><el-button :loading="filtering" :disabled="!activeQQ || isRunning || !petFriends.length" @click="filterFriends">{{ filtering ? '过滤中...' : '过滤好友' }}</el-button><el-button type="primary" :loading="refreshing" :disabled="!activeQQ || isRunning" @click="refresh">{{ refreshing ? '刷新中...' : '刷新好友列表' }}</el-button></div></div>
      <el-alert v-if="refreshState?.status === 'running'" class="pet-friends-alert" type="info" :closable="false" title="正在获取好友及宠物资料，好友列表会在完成后自动更新。" />
      <el-empty v-if="!friendsLoaded" description="点击“刷新好友列表”获取好友数据" :image-size="64" />
      <el-empty v-else-if="!petFriends.length" description="暂无拥有宠物的好友" :image-size="64" />
      <div v-else class="pet-friends-grid">
        <div v-for="friend in petFriends" :key="friend.friend_qq" class="pet-friend-item">
          <div class="pet-friend-header">
            <img class="pet-friend-avatar" :src="qqAvatarUrl(friend.friend_qq)" :alt="`${friend.nickname || friend.friend_qq} 的 QQ 头像`">
            <div class="pet-friend-info">
              <strong class="pet-friend-nickname">{{ friend.nickname || '未设置昵称' }}</strong>
              <span class="pet-friend-qq">QQ {{ friend.friend_qq }}</span>
            </div>
            <span class="pet-friend-status" :class="{ 'is-bound': friend.pet_id }">{{ friend.pet_id ? '已绑定' : '待绑定' }}</span>
          </div>
          <div class="pet-friend-meta">
            <div class="pet-friend-meta-row">
              <span>Pet ID</span>
              <strong :class="{ 'is-empty': !friend.pet_id }">{{ friend.pet_id || '尚未设置' }}</strong>
            </div>
            <div class="pet-friend-meta-row">
              <span>数据更新</span>
              <strong>{{ formatTime(friend.updated_at) }}</strong>
            </div>
          </div>
          <div class="pet-friend-actions">
            <el-button v-if="friend.pet_id" type="primary" size="small" @click="openProfile(friend)">查看宠物详情</el-button>
            <el-button v-else type="primary" plain size="small" @click="openBinding(friend)">设置 Pet ID</el-button>
            <el-button size="small" :loading="pokingQQ === friend.friend_qq" :disabled="Boolean(pokingQQ)" @click="pokeFriend(friend)">踩一踩</el-button>
          </div>
        </div>
      </div>
    </template>
  </el-card>

  <el-dialog v-model="bindingVisible" title="设置好友 Pet ID" width="min(460px, calc(100vw - 24px))">
    <p class="pet-friend-dialog-tip">为 {{ bindingFriend?.nickname || bindingFriend?.friend_qq }} 设置 Pet ID。系统会实时校验 Pet ID 是否属于该好友。</p>
    <el-input v-model="petID" placeholder="请输入 Pet ID" clearable @keyup.enter="bindFriend" />
    <template #footer><el-button @click="bindingVisible = false">取消</el-button><el-button type="primary" :loading="binding" :disabled="!petID.trim()" @click="bindFriend">校验并绑定</el-button></template>
  </el-dialog>

  <el-dialog v-model="profileVisible" title="好友宠物详情" width="min(620px, calc(100vw - 24px))">
    <div v-loading="profileLoading" class="pet-friend-profile">
      <div class="pet-friend-profile-owner"><img v-if="profileFriend" class="pet-friend-avatar" :src="qqAvatarUrl(profileFriend.friend_qq)" alt="好友头像"><div><strong>{{ profileFriend?.nickname || profileFriend?.friend_qq }}</strong><span>QQ：{{ profileValue(profile?.friend_uin) }}</span><span>Pet ID：{{ profileValue(profile?.pet_id) }}</span></div></div>
      <el-descriptions v-if="profile" :column="2" border>
        <el-descriptions-item label="数据来源">{{ profileValue(profile.source) }}</el-descriptions-item>
        <el-descriptions-item label="部分数据">{{ profile.partial ? '是' : '否' }}</el-descriptions-item>
        <el-descriptions-item label="清洁值">{{ profileValue(profile.vitals?.cleanliness) }}</el-descriptions-item>
        <el-descriptions-item label="饥饿值">{{ profileValue(profile.vitals?.hunger) }}</el-descriptions-item>
        <el-descriptions-item label="心情值">{{ profileValue(profile.vitals?.mood) }}</el-descriptions-item>
        <el-descriptions-item label="综合状态">{{ profileValue(profile.vitals?.total) }}</el-descriptions-item>
      </el-descriptions>
    </div>
  </el-dialog>
</template>
