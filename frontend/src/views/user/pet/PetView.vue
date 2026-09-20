<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { encouragePetActivity, getPetAccounts, getPetActivityOptions, getPetActivityOverview, getPetActivityStatus, getPetInteractionMessages, getPetMedalGallery, getPetProfile, startPetActivity, type PetAccount, type PetActivityCareer, type PetActivityOption, type PetActivityStatus, type PetActivityType, type PetFatigueStatus, type PetInteractionMessage, type PetMedal, type PetMedalGalleryItem, type PetProfileItem } from '../../../api/pet'
import { useRouter } from 'vue-router'

const router = useRouter()
const loading = ref(true)
const petLoading = ref(false)
const accounts = ref<PetAccount[]>([])
const pets = ref<PetProfileItem[]>([])
const activeQQ = ref('')
const activePet = computed<PetProfileItem | null>(() => pets.value.find(item => item.qq_number === activeQQ.value) || null)
const activityVisible = ref(false)
const medalVisible = ref(false)
const medalLoading = ref(false)
const medals = ref<PetMedalGalleryItem[]>([])
const interactionVisible = ref(false)
const interactionLoading = ref(false)
const interactionMessages = ref<PetInteractionMessage[]>([])
const activityLoading = ref(false)
const activityType = ref<PetActivityType>('school')
const activityOptions = ref<PetActivityOption[]>([])
const activityCareers = ref<PetActivityCareer[]>([])
const activityCareer = ref('')
const activityCareerType = ref<number | undefined>()

function activityCareerItems(value: unknown): PetActivityCareer[] {
  if (!Array.isArray(value)) return []
  return value.map(item => item && typeof item === 'object' ? item as PetActivityCareer : null).filter((item): item is PetActivityCareer => !!item && typeof item.career_type === 'number')
}

function careerAvailable(item: PetActivityCareer) { return item.status_code === undefined || item.status_code === 0 }

async function changeActivityCareer() {
  activityOptions.value = []
  await loadActivityOptions()
}
const startingActivity = ref<string | null>(null)
const activityLabels: Record<PetActivityType, string> = { school: '学习', work: '打工', adventure: '冒险' }
const activityStatus = ref<PetActivityStatus | null>(null)
const encouragingActivity = ref(false)
const activityRemainingSeconds = ref(0)
const activityStatusLoading = ref(false)
let activityCountdownTimer: ReturnType<typeof setInterval> | null = null

async function load() {
  loading.value = true
  try {
    const result = await getPetAccounts()
    accounts.value = result.data || []
    const firstAccount = accounts.value.find(account => !account.expired)
    if (firstAccount) {
      activeQQ.value = firstAccount.qq_number
      void loadPet(firstAccount.qq_number, true)
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : 'QQ 账号加载失败')
  } finally {
    loading.value = false
  }
}

async function loadPet(qq: string, force = false) {
  if (!force && pets.value.some(item => item.qq_number === qq)) return
  petLoading.value = true
  try {
    const result = await getPetProfile(qq)
    const account = accounts.value.find(item => item.qq_number === qq)
    pets.value = [...pets.value.filter(item => item.qq_number !== qq), { ...result.data as PetProfileItem, qq_nickname: account?.nickname || '' }]
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '宠物资料加载失败')
  } finally {
    petLoading.value = false
  }
}

function value(value: unknown, fallback = '未设置') {
  return typeof value === 'string' && value.trim() ? value : fallback
}

function medalRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' ? value as Record<string, unknown> : {}
}

function normalizeMedal(item: unknown): PetMedalGalleryItem {
  const record = medalRecord(item)
  const nested = medalRecord(record.medal)
  return { ...nested, ...record, medal: undefined }
}

function medalId(item: PetMedalGalleryItem, index: number) {
  return String(item.medal_id ?? item.id ?? index)
}

function medalName(item: PetMedalGalleryItem) {
  return value(item.name ?? item.medal_name, '未命名勋章')
}

function medalImage(item: PetMedalGalleryItem) {
  const image = item.image_url || item.icon_url
  return typeof image === 'string' && image.trim() ? image : ''
}

function medalDescription(item: PetMedalGalleryItem) {
  return value(item.description ?? item.requirement, '暂无描述')
}

function medalItems(data: unknown): PetMedalGalleryItem[] {
  if (Array.isArray(data)) return data.map(normalizeMedal)
  const record = medalRecord(data)
  if (Array.isArray(record.medals)) return record.medals.map(normalizeMedal)
  if (record.data !== data) return medalItems(record.data)
  return []
}

function petAttributes(item: PetProfileItem) {
  return Array.isArray(item.attributes?.attributes)
    ? item.attributes.attributes
    : []
}

function overviewPet(qq: string) {
  return pets.value.find(item => item.qq_number === qq)
}

function metricChange(item: PetProfileItem, name: string) {
  return name === '金币'
    ? item.metric_changes?.gold || 0
    : item.metric_changes?.attributes?.[name] || 0
}

function changeText(change: number) {
  return change > 0 ? `+${change}` : String(change)
}

function dominantTypeLabel(value: number | null) {
  if (value === null) return '未获取'
  return ({ 1: '智力型', 2: '力量型', 3: '魅力型', 10: '综合型' } as Record<number, string>)[value] || String(value)
}

function fatigueStatus(value: unknown): PetFatigueStatus {
  const item = value && typeof value === 'object' ? value as Record<string, unknown> : {}
  const todayHours = Number(item.todayHours)
  const schoolHours = Number(item.schoolHours)
  const workHours = Number(item.workHours)
  return {
    todayHours: Number.isFinite(todayHours) ? todayHours : 0,
    schoolHours: Number.isFinite(schoolHours) ? schoolHours : 0,
    workHours: Number.isFinite(workHours) ? workHours : 0,
  }
}

function fatigueClass(value: unknown) {
  const hours = fatigueStatus(value).todayHours
  if (hours >= 12) return 'is-severe'
  if (hours >= 8) return 'is-warning'
  return 'is-normal'
}

function fatigueText(value: unknown) {
  const hours = fatigueStatus(value).todayHours
  if (hours >= 12) return '重度疲劳'
  if (hours >= 8) return '疲劳'
  return '正常'
}

function fatigueHoursText(value: unknown) {
  const status = fatigueStatus(value)
  return `今日学习 ${formatHours(status.schoolHours)} 小时 · 打工 ${formatHours(status.workHours)} 小时`
}

function formatHours(hours: number) {
  return Number.isInteger(hours) ? String(hours) : hours.toFixed(2).replace(/0+$/, '').replace(/\.$/, '')
}

function activityLabel(type: string) {
  return activityLabels[type as PetActivityType] || type
}

function cleanActivityText(value: unknown, fallback = '暂无') {
  if (typeof value !== 'string') return fallback
  let cleaned = value
  let start = cleaned.indexOf('![')
  while (start >= 0) {
    const labelEnd = cleaned.indexOf(']', start + 2)
    const urlStart = labelEnd >= 0 ? cleaned.indexOf('(', labelEnd + 1) : -1
    if (labelEnd < 0 || urlStart < 0) break
    let depth = 0
    let end = -1
    for (let index = urlStart; index < cleaned.length; index += 1) {
      if (cleaned[index] === '(') depth += 1
      if (cleaned[index] === ')') {
        depth -= 1
        if (depth === 0) {
          end = index + 1
          break
        }
      }
    }
    if (end < 0) break
    cleaned = `${cleaned.slice(0, start)}${cleaned.slice(end)}`
    start = cleaned.indexOf('![', start)
  }
  cleaned = cleaned.replace(/\[\s*\]\([^)]*\)/g, '')
  cleaned = cleaned.replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
  cleaned = cleaned.replace(/<img\b[^>]*>/gi, '').replace(/\s+/g, ' ').trim()
  return cleaned || fallback
}

function formatActivityRemaining(seconds: number) {
  const safeSeconds = Math.max(0, Math.floor(seconds))
  const minutes = Math.floor(safeSeconds / 60)
  const remaining = safeSeconds % 60
  return `${minutes}:${String(remaining).padStart(2, '0')}`
}

function stopActivityCountdown() {
  if (activityCountdownTimer) {
    clearInterval(activityCountdownTimer)
    activityCountdownTimer = null
  }
}

function startActivityCountdown() {
  stopActivityCountdown()
  if (!activityVisible.value || activityRemainingSeconds.value <= 0) return
  activityCountdownTimer = setInterval(() => {
    if (activityRemainingSeconds.value <= 1) {
      activityRemainingSeconds.value = 0
      stopActivityCountdown()
      return
    }
    activityRemainingSeconds.value -= 1
  }, 1000)
}

function numberValue(value: unknown) {
  return typeof value === 'number' || (typeof value === 'string' && value.trim()) ? String(value) : '暂无数据'
}

function vitalValue(value: unknown) {
  return typeof value === 'number' ? Math.max(0, Math.min(100, value)) : 0
}

async function loadActivityOptions() {
  if (!activePet.value) return
  activityLoading.value = true
  try {
    if (activityType.value === 'work' && activityCareerType.value === undefined) {
      const overview = await getPetActivityOverview(activePet.value.qq_number, 'work')
      activityCareers.value = activityCareerItems(overview.data?.entries).filter(item => item.name !== '???')
      const preferred = overview.data?.current_career_type
      const firstAvailable = activityCareers.value.find(careerAvailable)
      activityCareerType.value = activityCareers.value.some(item => item.career_type === preferred && careerAvailable(item)) ? preferred : firstAvailable?.career_type
      activityCareer.value = ''
      if (activityCareerType.value === undefined) return
    }
    if (activityType.value !== 'work') {
      activityCareerType.value = undefined
      activityCareers.value = []
    }
    const result = await getPetActivityOptions(activePet.value.qq_number, activityType.value, activityCareerType.value)
    activityOptions.value = result.data?.options || []
    activityCareer.value = result.data?.career_name || ''
  } catch (error) {
    activityOptions.value = []
    ElMessage.error(error instanceof Error ? error.message : '活动选项加载失败')
  } finally {
    activityLoading.value = false
  }
}

function interactionEventLabel(type: number) {
  return ({ 1: '喂食', 2: '来访', 4: '抚摸', 6900: '宠物 PK' } as Record<number, string>)[type] || `互动 ${type}`
}

function interactionEventType(type: number) {
  return ({ 1: 'success', 2: 'primary', 4: 'warning', 6900: 'danger' } as const)[type as 1 | 2 | 4 | 6900] || 'info'
}

function formatInteractionTime(timestamp: number) {
  if (!Number.isFinite(timestamp)) return '时间未知'
  return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(timestamp))
}

function qqAvatarUrl(qq: string) {
  return `https://q1.qlogo.cn/g?b=qq&nk=${encodeURIComponent(qq)}&s=100`
}

async function openInteractions() {
  if (!activePet.value) return
  interactionVisible.value = true
  interactionLoading.value = true
  interactionMessages.value = []
  try {
    const result = await getPetInteractionMessages(activePet.value.qq_number)
    interactionMessages.value = Array.isArray(result.data?.messages) ? result.data.messages : []
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '互动记录加载失败')
  } finally {
    interactionLoading.value = false
  }
}

async function openMedals() {
  if (!activePet.value) return
  medalVisible.value = true
  medalLoading.value = true
  try {
    const result = await getPetMedalGallery(activePet.value.qq_number)
    medals.value = medalItems(result.data)
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '宠物勋章加载失败')
  } finally {
    medalLoading.value = false
  }
}

async function openActivity() {
  if (!activePet.value) return
  activityVisible.value = true
  await Promise.all([loadActivityOptions(), loadActivityStatus()])
}

async function loadActivityStatus() {
  if (!activePet.value) return
  activityStatusLoading.value = true
  try {
    const result = await getPetActivityStatus(activePet.value.qq_number)
    activityStatus.value = result.data
    activityRemainingSeconds.value = Math.max(0, Number(result.data?.remaining_seconds || 0))
    startActivityCountdown()
  } catch (error) {
    activityStatus.value = null
    activityRemainingSeconds.value = 0
    stopActivityCountdown()
    ElMessage.error(error instanceof Error ? error.message : '活动状态加载失败')
  } finally {
    activityStatusLoading.value = false
  }
}

async function encourageActivity() {
  if (!activePet.value) return
  encouragingActivity.value = true
  try {
    const result = await encouragePetActivity(activePet.value.qq_number)
    ElMessage.success('鼓励成功')
    await loadActivityStatus()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '鼓励失败')
  } finally {
    encouragingActivity.value = false
  }
}

async function startActivity(option: PetActivityOption) {
  if (!activePet.value || option.can_do !== true || !option.name) return
  startingActivity.value = option.name
  try {
    const result = await startPetActivity(activePet.value.qq_number, activityType.value, option.name, option.sub_event_type)
    ElMessage.success('活动开始成功')
    activityVisible.value = false
    stopActivityCountdown()
    if (result.data?.story_id) ElMessage.info(`已保存活动进度：${result.data.story_id}`)
    await loadPet(activePet.value.qq_number, true)
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '活动开始失败')
  } finally {
    startingActivity.value = null
  }
}


function selectQQ(qq: string) {
  activeQQ.value = qq
  const account = accounts.value.find(item => item.qq_number === qq)
  if (!account?.expired) void loadPet(qq, true)
  else if (!overviewPet(qq)) void loadPet(qq, true)
}

function backToPetList() {
  activeQQ.value = ''
  activityVisible.value = false
  stopActivityCountdown()
}

onMounted(load)
onUnmounted(stopActivityCountdown)
</script>

<template>
  <div class="page-heading">
    <div><h1>QQ 宠物</h1></div>
  </div>
  <el-card v-loading="loading" class="pet-page-card">
    <el-empty v-if="!accounts.length && !loading" description="暂无在线且已绑定的 QQ 宠物" />
    <template v-else>
      <div class="pet-account-switcher" role="group" aria-label="切换 QQ 账号">
        <button v-for="account in accounts" :key="account.qq_number" type="button" class="pet-account-button" :class="{ 'is-active': activeQQ === account.qq_number }" @click="selectQQ(account.qq_number)">
          <span class="pet-account-button-name">{{ value(account.nickname, account.qq_number) }}</span>
          <span class="pet-account-button-qq">QQ {{ account.qq_number }}</span>
          <span class="pet-account-button-status" :class="account.online ? 'is-online' : 'is-offline'">{{ account.online ? '在线' : '离线' }}</span>
        </button>
      </div>
      <div v-if="!activePet" v-loading="petLoading" class="pet-empty-state"><el-empty description="暂无宠物资料" /></div>
      <div v-else class="pet-detail-view">
        <div class="pet-detail-toolbar"><span>{{ value(activePet.qq_nickname, activePet.qq_number) }} · {{ activePet.qq_number }}</span></div>
      <div class="pet-profile">
            <div class="pet-profile-header">
            <el-avatar :size="96" :src="value(activePet.profile.avatar_url, '')">{{ value(activePet.profile.pet_name, '宠') }}</el-avatar>
            <div class="pet-profile-title"><div><h2>{{ value(activePet.profile.pet_name) }}</h2><p class="pet-species-line">{{ value(activePet.profile.species) }}<small> · ID {{ value(String(activePet.profile.pet_id || '')) }}</small></p></div><div class="pet-profile-summary"><span><small>职业</small><strong>{{ value(activePet.profile.job_name) }}</strong></span><span><small>性格</small><strong>{{ value(activePet.profile.personality) }}</strong></span><span><small>类型</small><strong>{{ dominantTypeLabel(activePet.dominant_type) }}</strong></span></div></div>
          </div>
          <div class="pet-metrics">
            <div class="pet-vitals-title">成长与财富</div>
            <div class="pet-metrics-grid">
              <div v-for="attribute in petAttributes(activePet)" :key="String(attribute.group ?? attribute.name)" class="pet-metric-card">
                <span class="pet-metric-label">{{ attribute.name || '属性' }}</span>
                <strong>{{ attribute.value ?? 0 }}</strong>
                <small v-if="metricChange(activePet, attribute.name || '')" class="pet-metric-change" :class="metricChange(activePet, attribute.name || '') > 0 ? 'is-up' : 'is-down'">{{ changeText(metricChange(activePet, attribute.name || '')) }}</small>
              </div>
              <div class="pet-metric-card is-gold">
                <span class="pet-metric-label">金币</span>
                <strong>{{ numberValue(activePet.vitals?.gold) }}</strong>
                <small v-if="metricChange(activePet, '金币')" class="pet-metric-change" :class="metricChange(activePet, '金币') > 0 ? 'is-up' : 'is-down'">{{ changeText(metricChange(activePet, '金币')) }}</small>
              </div>
              <div class="pet-metric-card is-power">
                <span class="pet-metric-label">战力</span>
                <strong>{{ numberValue(activePet.power) }}</strong>
                <small v-if="activePet.power_change" class="pet-metric-change" :class="activePet.power_change > 0 ? 'is-up' : 'is-down'">{{ changeText(activePet.power_change) }}</small>
              </div>
              <div class="pet-metric-card fatigue-metric" :class="fatigueClass(activePet.fatigue)">
                <span class="pet-metric-label">疲劳状态</span>
                <strong>{{ fatigueText(activePet.fatigue) }}</strong>
                <small>{{ fatigueHoursText(activePet.fatigue) }}</small>
              </div>
            </div>
          </div>
          <div class="pet-vitals">
            <div class="pet-vitals-title">宠物状态</div>
            <div class="pet-vitals-grid">
              <div v-for="status in [{ label: '心情', value: activePet.vitals?.mood, color: '#7b93cf' }, { label: '饥饿', value: activePet.vitals?.hunger, color: '#e4a45f' }, { label: '清洁', value: activePet.vitals?.cleanliness, color: '#73b6a2' }, { label: '综合状态', value: activePet.vitals?.total, color: '#8c82c5' }]" :key="status.label" class="pet-vital-item"><el-progress type="dashboard" :percentage="vitalValue(status.value)" :color="status.color" :width="92" :stroke-width="9" /><span>{{ status.label }}</span><strong>{{ numberValue(status.value) }}<small>/100</small></strong></div>
            </div>
          </div>
          <div class="pet-action-bar">
            <el-button type="success" plain @click="router.push({ path: '/pet/auto-control', query: { qq: activePet.qq_number } })">自动控制</el-button>
            <el-button type="primary" plain @click="openMedals">宠物勋章</el-button>
            <el-button type="info" plain @click="openInteractions">互动记录</el-button>
            <el-button type="primary" plain @click="openActivity">互动测试</el-button>
            <el-button type="warning" plain @click="router.push({ path: '/pet/daily-stats', query: { qq: activePet.qq_number } })">每日变化</el-button>
            <el-button type="primary" plain @click="router.push({ path: '/pet/friends', query: { qq: activePet.qq_number } })">好友管理</el-button>
          </div>
        </div>
      </div>
    </template>
  </el-card>
  <el-dialog v-model="medalVisible" title="宠物勋章" width="min(760px, calc(100vw - 32px))">
    <div v-loading="medalLoading" class="pet-medal-gallery">
      <el-empty v-if="!medals.length && !medalLoading" description="暂无宠物勋章" />
      <el-tooltip v-for="(item, index) in medals" :key="medalId(item, index)" :content="medalDescription(item)" placement="top">
        <div class="pet-medal-item" :class="{ 'is-locked': !item.acquired, 'is-equipped': item.equipped }">
          <img v-if="medalImage(item)" :src="medalImage(item)" :alt="medalName(item)">
          <span v-else>{{ medalName(item).slice(0, 1) }}</span>
          <strong>{{ medalName(item) }}</strong>
        </div>
      </el-tooltip>
    </div>
  </el-dialog>
  <el-dialog v-model="interactionVisible" width="min(780px, calc(100vw - 24px))" class="pet-interaction-dialog" align-center>
    <template #header>
      <div class="pet-interaction-dialog-title">
        <img v-if="activePet" class="pet-interaction-title-avatar" :src="qqAvatarUrl(activePet.qq_number)" :alt="`${activePet.qq_number} 的 QQ 头像`">
        <div><strong>互动记录</strong><small>{{ value(activePet?.profile.pet_name, '宠物') }} 的访客动态</small></div>
      </div>
    </template>
    <div v-loading="interactionLoading" class="pet-interaction-content">
      <div class="pet-interaction-summary">
        <span>最近互动</span>
        <span class="pet-interaction-count">共 <strong>{{ interactionMessages.length }}</strong> 条记录</span>
      </div>
      <el-empty v-if="!interactionMessages.length && !interactionLoading" description="暂时还没有好友来互动" :image-size="88" />
      <div v-else class="pet-interaction-list">
        <article v-for="message in interactionMessages" :key="message.message_id" class="pet-interaction-item">
          <img class="pet-interaction-avatar" :src="qqAvatarUrl(message.user_id)" :alt="`${value(message.pet_name, '访客')}的 QQ 头像`">
          <div class="pet-interaction-body">
            <header>
              <div class="pet-interaction-user"><strong>{{ value(message.pet_name, '神秘访客') }}</strong><small>QQ {{ message.user_id }}</small></div>
              <time>{{ formatInteractionTime(message.timestamp) }}</time>
            </header>
            <p class="pet-interaction-message"><template v-for="(segment, index) in message.segments" :key="index"><strong v-if="segment.font_weight === '2'">{{ segment.text }}</strong><span v-else>{{ segment.text }}</span></template></p>
          </div>
          <el-tag class="pet-interaction-event" :type="interactionEventType(message.event_type)" size="small" effect="light" round>{{ interactionEventLabel(message.event_type) }}</el-tag>
        </article>
      </div>
    </div>
  </el-dialog>
  <el-dialog v-model="activityVisible" title="互动测试" width="min(760px, calc(100vw - 32px))" class="pet-activity-dialog">
    <div v-loading="activityLoading" class="pet-activity-content">
      <div class="pet-activity-status">
        <div><span>当前活动状态</span><strong>{{ activityStatus?.story_id ? `进行中 · ${cleanActivityText(activityStatus.story_id)}` : '暂无进行中的活动' }}</strong></div>
        <div class="pet-activity-status-meta"><span v-if="activityStatusLoading">状态加载中</span><span v-else-if="activityStatus?.story_id">剩余 {{ formatActivityRemaining(activityRemainingSeconds) }}</span><span v-else>暂无倒计时</span><el-button size="small" :loading="activityStatusLoading" @click="loadActivityStatus">刷新</el-button><el-button v-if="activityStatus?.story_id" size="small" :loading="encouragingActivity" @click="encourageActivity">鼓励</el-button></div>
      </div>
      <div class="pet-activity-types">
        <el-button v-for="(label, type) in activityLabels" :key="type" :type="activityType === type ? 'primary' : 'default'" @click="activityType = type; loadActivityOptions()">{{ label }}</el-button>
      </div>
      <div v-if="activityType === 'work'" class="pet-activity-career-selector">
        <span>职业分类</span>
        <el-select v-model="activityCareerType" placeholder="选择职业分类" @change="changeActivityCareer">
          <el-option v-for="career in activityCareers" :key="career.career_type" :label="career.name || `职业 ${career.career_type}`" :value="career.career_type" :disabled="!careerAvailable(career)">
            <span>{{ career.name || `职业 ${career.career_type}` }}</span><small v-if="!careerAvailable(career)"> · {{ career.message || '暂不可用' }}</small>
          </el-option>
        </el-select>
      </div>
      <div class="pet-activity-career">{{ cleanActivityText(activityCareer, '当前活动') }}</div>
      <el-empty v-if="!activityOptions.length && !activityLoading" description="暂无可进行的活动" :image-size="72" />
      <div v-else class="pet-activity-options">
        <div v-for="(option, index) in activityOptions" :key="option.name || index" class="pet-activity-option" :class="{ 'is-unavailable': option.can_do !== true }" @click="startActivity(option)">
          <div class="pet-item-image pet-activity-icon"><img v-if="option.icon_url" :src="option.icon_url" :alt="cleanActivityText(option.name, '活动')"><span v-else>活</span></div>
          <div class="pet-activity-option-body">
            <div class="pet-activity-option-head"><strong>{{ cleanActivityText(option.name, '未命名活动') }}</strong><el-tag v-if="option.can_do === true" type="success" size="small">可以进行</el-tag><el-tag v-else type="info" size="small">暂不可用</el-tag></div>
            <div class="pet-activity-meta"><span>消耗：{{ cleanActivityText(option.cost) }}</span><span>时间：{{ cleanActivityText(option.duration) }}</span><span>获得：{{ cleanActivityText(option.reward) }}</span></div>
            <p>{{ cleanActivityText(option.description) }}</p>
            <small v-if="option.can_do !== true">{{ cleanActivityText(option.unavailable_reason, '当前无法进行') }}</small>
          </div>
          <el-button v-if="option.can_do === true" type="primary" size="small" :loading="startingActivity === option.name" @click.stop="startActivity(option)">开始</el-button>
        </div>
      </div>
    </div>
  </el-dialog>
</template>
