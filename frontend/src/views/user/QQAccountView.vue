<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import QRCode from 'qrcode'
import { addQQ, checkQQSecuritySMS, confirmQQIdentitySMS, confirmQQSecurity, createQQSecurityQR, getQQAccountOptions, getQQAccountPrice, getQQNodes, getQQPlans, getQQSecurityMethods, getQQSecuritySMS, getUserQQAccountInfo, getUserQQAccounts, loginQQ, offlineQQ, queryQQSecurityQRStatus, renewQQPlan, submitQQIdentityCaptcha, submitQQIdentityPhone, submitQQSecuritySlider, updateQQ, type QQAccountOption, type QQLoginResult, type QQNode, type QQPlan, type UserQQAccount } from '../../api/qq'
import { formatDateTime } from '../../utils/format'
import { getWallet } from '../../api/wallet'

const loading = ref(true)
const accounts = ref<UserQQAccount[]>([])
const addLoading = ref(false)
const addDialogVisible = ref(false)
const optionsLoading = ref(false)
const protocols = ref<QQAccountOption[]>([])
const deviceProfiles = ref<QQAccountOption[]>([])
const editProtocols = ref<QQAccountOption[]>([])
const editDeviceProfiles = ref<QQAccountOption[]>([])
const nodes = ref<QQNode[]>([])
const monthlyPrice = ref(0)
const plans = ref<QQPlan[]>([])
const walletCoins = ref(0)
const infoLoading = ref(false)
const loginQQLoading = ref<string | null>(null)
const offlineQQLoading = ref<string | null>(null)
const renewQQLoading = ref<string | null>(null)
const selectedInfo = ref<Record<string, unknown> | null>(null)
const infoDialogVisible = ref(false)
const renewDialogVisible = ref(false)
const renewAccountTarget = ref<UserQQAccount | null>(null)
const renewPlanID = ref(1)
const editDialogVisible = ref(false)
const editLoading = ref(false)
const editForm = ref({ qq_number: '', password: '', protocol_id: null as number | null, device_profile_id: null as number | null, node_id: null as number | null })
const securityDialogVisible = ref(false)
const securityLoading = ref(false)
const securityAccount = ref<UserQQAccount | null>(null)
const securityResult = ref<QQLoginResult | null>(null)
const securityMethods = ref<Record<string, unknown> | null>(null)
const securityURL = ref('')
const securityQRCode = ref('')
const qrResult = ref<Record<string, unknown> | null>(null)
const qrCodeImage = ref('')
const qrToken = ref('')
const smsType = ref<number | null>(null)
const smsSign = ref('')
const smsCode = ref('')
const smsMessage = ref('')
const smsTarget = ref('')
const sliderLoading = ref(false)
const sliderURL = ref('')
const sliderFrameURL = ref('')
const identityURL = ref('')
const identityFrameURL = ref('')
const identityStage = ref<'captcha' | 'phone' | 'sms'>('captcha')
const identityAreaCode = ref('+86')
const identityMobile = ref('')
const identityMaskedMobile = ref('')
const identitySMSTarget = ref('')
const identitySMSContent = ref('')
let qrTimer: ReturnType<typeof setInterval> | null = null
let qrExpiryTimer: ReturnType<typeof setTimeout> | null = null
const qrPolling = ref(false)
const addForm = ref({ qq_number: '', password: '', protocol_id: null as number | null, device_profile_id: null as number | null, plan_id: 1, node_id: null as number | null })
const selectedAddPlan = computed(() => plans.value.find(plan => plan.id === addForm.value.plan_id) || plans.value[0])
const selectedRenewPlan = computed(() => plans.value.find(plan => plan.id === renewPlanID.value) || plans.value[0])
const totalPrice = computed(() => selectedAddPlan.value?.price || 0)
const renewTotalPrice = computed(() => selectedRenewPlan.value?.price || 0)
const statusMap: Record<number, { label: string; type: 'info' | 'success' | 'warning' }> = { 0: { label: '离线', type: 'info' }, 1: { label: '在线', type: 'success' }, 2: { label: '登录中', type: 'warning' } }
function statusInfo(status: number) { return statusMap[status] || { label: '未知', type: 'info' as const } }
function protocolName(id: unknown) { return protocols.value.find(item => item.id === Number(id))?.name || '当前账号配置未返回' }
function deviceProfileName(id: unknown) { return deviceProfiles.value.find(item => item.id === Number(id))?.name || '当前账号配置未返回' }
function nodeLabel(node: QQNode) {
  const proxy = node.proxy_enabled ? ` · ${node.proxy_type || '代理'}` : ''
  return `${node.name} · ${node.online_account_count}/${node.account_count} 在线${proxy}${node.enabled ? '' : ' · 已停用'}`
}
function qqAvatarUrl(qq: string) { return `https://q1.qlogo.cn/g?b=qq&nk=${encodeURIComponent(qq)}&s=100` }
function openAddDialog() {
  addDialogVisible.value = true
}
async function showInfo(account: UserQQAccount) {
  infoLoading.value = true
  try {
    const result = await getUserQQAccountInfo(account.qq_number)
    selectedInfo.value = result.data
    infoDialogVisible.value = true
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : 'QQ 信息加载失败') } finally { infoLoading.value = false }
}
async function loginAccount(account: UserQQAccount) {
  if (account.expired) { ElMessage.warning('QQ 服务已到期，请先续费'); return }
  loginQQLoading.value = account.qq_number
  try {
    const result = await loginQQ(account.qq_number)
    if (result.data) await handleLoginResult(account, result.data)
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : 'QQ 登录失败') } finally { loginQQLoading.value = null }
}
async function handleLoginResult(account: UserQQAccount, result: QQLoginResult, successMessage = 'QQ 登录成功') {
  if (result.code === 0) {
    ElMessage.success(successMessage)
    securityDialogVisible.value = false
    await load()
  } else if (result.code === 140022008 && result.slider_url) {
    await openSliderDialog(account, result)
  } else if (result.code === 140022010) {
    await openSecurityDialog(account, result)
  } else if (result.code === 140022007 && result.identity_url) {
    await openIdentityDialog(account, result)
  } else {
    const url = result.slider_url || result.identity_url || result.security_url
    if (url) openSecurityURL(url)
    ElMessage.warning(result.message || 'QQ 登录需要进一步验证')
  }
}
async function openIdentityDialog(account: UserQQAccount, result: QQLoginResult) {
  securityAccount.value = account
  securityResult.value = result
  securityMethods.value = null
  identityURL.value = cleanSecurityURL(result.identity_url)
  identityFrameURL.value = identityURL.value ? `/login-identity-captcha.html?url=${encodeURIComponent(identityURL.value)}` : ''
  identityStage.value = 'captcha'
  identityAreaCode.value = '+86'
  identityMobile.value = ''
  identityMaskedMobile.value = ''
  identitySMSTarget.value = ''
  identitySMSContent.value = ''
  securityDialogVisible.value = true
  await nextTick()
}
async function openSliderDialog(account: UserQQAccount, result: QQLoginResult) {
  securityAccount.value = account
  securityResult.value = result
  securityMethods.value = null
  qrResult.value = null
  qrCodeImage.value = ''
  smsType.value = null
  smsSign.value = ''
  smsCode.value = ''
  smsMessage.value = ''
  smsTarget.value = ''
  sliderURL.value = result.slider_url || ''
  sliderFrameURL.value = ''
  identityURL.value = ''
  identityFrameURL.value = ''
  securityDialogVisible.value = true
  await prepareSliderCaptcha()
}
async function prepareSliderCaptcha() {
  if (!sliderURL.value) { ElMessage.warning('萌卡未返回滑块验证地址'); return }
  sliderLoading.value = true
  sliderFrameURL.value = ''
  try {
    const url = new URL(sliderURL.value)
    const isLinuxSlider = url.hostname.toLowerCase() === 'ti.qq.com' && url.pathname === '/safe/tools/captcha/sms-verify-login'
    sliderFrameURL.value = `/${isLinuxSlider ? 'login-linux-slider-captcha' : 'login-slider-captcha'}.html?url=${encodeURIComponent(sliderURL.value)}&qq=${encodeURIComponent(securityAccount.value?.qq_number || '')}`
  } catch {
    ElMessage.error('滑块验证地址无效')
  } finally { sliderLoading.value = false }
}
async function submitSliderResult(result: { ret: number; ticket?: string; randstr?: string }) {
  if (!securityAccount.value || result.ret !== 0 || !result.ticket || !result.randstr) return
  if (sliderLoading.value) return
  sliderLoading.value = true
  try {
    const response = await submitQQSecuritySlider(securityAccount.value.qq_number, result.ticket, result.randstr)
    const loginResult = response.data
    if (loginResult?.code === 0) {
      ElMessage.success('QQ 登录成功')
      securityDialogVisible.value = false
      await load()
    } else if (loginResult?.code === 140022010) {
      securityResult.value = loginResult
      securityMethods.value = loginResult.security_verify || null
      securityURL.value = cleanSecurityURL(loginResult.security_url)
      if (!securityMethods.value) await refreshSecurityMethods()
    } else if (loginResult?.code === 140022007) {
      securityResult.value = loginResult
      identityURL.value = cleanSecurityURL(loginResult.identity_url)
      identityFrameURL.value = identityURL.value ? `/login-identity-captcha.html?url=${encodeURIComponent(identityURL.value)}` : ''
      identityStage.value = 'captcha'
      if (!identityURL.value) ElMessage.warning(loginResult.message || '需要身份验证，但未返回身份验证地址')
    } else if (loginResult?.code === 140022008) {
      securityResult.value = loginResult
      sliderURL.value = loginResult.slider_url || sliderURL.value
      await prepareSliderCaptcha()
    } else {
      ElMessage.warning(loginResult?.message || '滑块验证未通过')
    }
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '滑块验证提交失败') } finally { sliderLoading.value = false }
}
function handleSliderCaptchaMessage(event: MessageEvent<unknown>) {
  if (event.origin !== window.location.origin || !securityDialogVisible.value || !securityAccount.value) return
  const data = event.data
  if (!data || typeof data !== 'object') return
  const result = data as { type?: unknown; ticket?: unknown; randstr?: unknown }
  if (typeof result.ticket !== 'string' || typeof result.randstr !== 'string') return
  if (result.type === 'login-slider-captcha') void submitSliderResult({ ret: 0, ticket: result.ticket, randstr: result.randstr })
  if (result.type === 'login-identity-captcha') void submitIdentityCaptchaResult(result.ticket, result.randstr)
}
function findString(value: unknown, keys: string[]): string {
  if (!value || typeof value !== 'object') return ''
  const object = value as Record<string, unknown>
  for (const key of keys) {
    const current = object[key]
    if (current !== undefined && current !== null && String(current).trim()) return String(current).trim()
  }
  for (const current of Object.values(object)) {
    const nested = findString(current, keys)
    if (nested) return nested
  }
  return ''
}
function securityResultState(value: unknown) {
  if (!value || typeof value !== 'object') return 0
  const object = value as Record<string, unknown>
  const result = object.result as Record<string, unknown> | undefined
  return Number(result?.state ?? result?.code ?? 0)
}
async function submitIdentityCaptchaResult(ticket: string, randstr: string) {
  if (!securityAccount.value || securityLoading.value) return
  securityLoading.value = true
  try {
    const result = await submitQQIdentityCaptcha(securityAccount.value.qq_number, ticket, randstr)
    identityMaskedMobile.value = findString(result.data, ['mobile', 'phone', 'maskMobile', 'mobileMask', 'phoneMask', 'displayMobile'])
    const areaCode = findString(result.data, ['areaCode', 'area_code', 'countryCode', 'country_code', 'nationCode', 'nation_code'])
    identityAreaCode.value = areaCode ? `+${areaCode.replace(/^\+/, '')}` : '+86'
    identityStage.value = 'phone'
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '身份验证提交失败') } finally { securityLoading.value = false }
}
async function requestIdentitySMS() {
  if (!securityAccount.value || !identityMobile.value.trim()) { ElMessage.warning('请输入绑定手机号'); return }
  securityLoading.value = true
  try {
    const result = await submitQQIdentityPhone(securityAccount.value.qq_number, identityMobile.value.trim(), identityAreaCode.value)
    identitySMSTarget.value = findString(result.data, ['sendTo', 'send_to', 'to', 'phone', 'mobile', 'smsPhone'])
    identitySMSContent.value = findString(result.data, ['sms', 'content', 'text', 'msg', 'message'])
    identityStage.value = 'sms'
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '身份验证短信请求失败') } finally { securityLoading.value = false }
}
async function confirmIdentitySMS() {
  if (!securityAccount.value || !identityMobile.value.trim()) return
  securityLoading.value = true
  try {
    const account = securityAccount.value
    const result = await confirmQQIdentitySMS(account.qq_number, identityMobile.value.trim(), identityAreaCode.value)
    if (result.data) await handleLoginResult(account, result.data)
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '身份验证短信确认失败') } finally { securityLoading.value = false }
}
async function copyIdentitySMSMessage() {
  if (!identitySMSContent.value) return
  try {
    await navigator.clipboard.writeText(identitySMSContent.value)
    ElMessage.success('短信内容已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选择短信内容')
  }
}
function cleanSecurityURL(value?: string) {
  const url = value?.trim()
  if (!url) return ''
  if (/^https?:\/\//i.test(url)) return url.replace(/^http:\/\//i, 'https://')
  return `https://${url.replace(/^\/\//, '')}`
}
function isEmbeddedQQSecurityURL(value: unknown) {
  if (typeof value !== 'string' || !value.trim()) return false
  try {
    const url = new URL(value)
    return url.protocol === 'https:' && url.hostname.toLowerCase() === 'accounts.qq.com' && url.pathname.toLowerCase() === '/login/attack'
  } catch {
    return false
  }
}
async function openSecurityDialog(account: UserQQAccount, result: QQLoginResult) {
  securityAccount.value = account
  securityResult.value = result
  securityMethods.value = result.security_verify || null
  securityURL.value = cleanSecurityURL(result.security_url)
  qrResult.value = null
  qrCodeImage.value = ''
  qrToken.value = ''
  smsType.value = null
  smsSign.value = ''
  smsCode.value = ''
  smsMessage.value = ''
  smsTarget.value = ''
  securityDialogVisible.value = true
  if (!securityMethods.value) await refreshSecurityMethods()
}
async function refreshSecurityMethods() {
  if (!securityAccount.value) return
  securityLoading.value = true
  try {
    const result = await getQQSecurityMethods(securityAccount.value.qq_number)
    securityMethods.value = result.data
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '安全验证方式查询失败') } finally { securityLoading.value = false }
}
function securityMethodData() { return (securityMethods.value?.methods as Record<string, unknown> | undefined) || securityMethods.value || {} }
function verifyList() {
  const value = securityMethodData().verify_list
  return Array.isArray(value) ? value.map(Number).filter(Number.isFinite) : []
}
function availableVerifyMethods() {
  return verifyList().map(type => {
    if (type === 10) return { type, label: '扫码验证', description: '使用手机扫码确认', action: startQR }
    if (type === 4) return { type, label: '接收短信', description: '向密保手机发送验证码', action: () => requestSMS(4) }
    if (type === 3) return { type, label: '发送短信', description: '使用密保手机发送指定内容', action: () => requestSMS(3) }
    return { type, label: `验证方式 ${type}`, description: '当前页面暂不支持此验证方式', action: () => ElMessage.warning(`暂不支持验证方式 ${type}`), disabled: true }
  })
}
function qrStatusLabel(value: unknown) {
  if (value === 'scanned') return '扫码成功，等待手机确认'
  if (value === 'confirmed') return '验证成功，正在继续登录'
  if (value === 'expired') return '二维码已失效，请重新获取'
  return '等待扫码'
}
function stopQRPolling() {
  if (qrTimer) clearInterval(qrTimer)
  if (qrExpiryTimer) clearTimeout(qrExpiryTimer)
  qrTimer = null
  qrExpiryTimer = null
}
watch(securityURL, async (url) => {
  securityQRCode.value = url && !isEmbeddedQQSecurityURL(url) ? await QRCode.toDataURL(url) : ''
}, { immediate: true })
async function startQR() {
  if (!securityAccount.value) return
  securityLoading.value = true
  try {
    stopQRPolling()
    smsType.value = null
    smsSign.value = ''
    smsCode.value = ''
    smsMessage.value = ''
    smsTarget.value = ''
    qrResult.value = null
    qrCodeImage.value = ''
    qrToken.value = ''
    const result = await createQQSecurityQR(securityAccount.value.qq_number)
    const data = result.data || {}
    const createResponse = data.create_rsp as Record<string, unknown> | undefined
    const sourceURL = String(createResponse?.str_url || data.qr_url || '')
    qrToken.value = String(data.guarantee_token || createResponse?.guarantee_token || createResponse?.str_guarantee_token || (sourceURL ? window.btoa(sourceURL) : ''))
    const qrURL = sourceURL ? `https://accounts.qq.com/safe/scanresult?${new URLSearchParams({ _wv: '3', _wwv: '1', str_url: sourceURL, envfrom: 'double-check', verify_id: 'undefined', verify_scene: 'undefined', uin: securityAccount.value.qq_number }).toString()}` : ''
    qrResult.value = { ...data, qr_url: qrURL, status: 'waiting' }
    qrCodeImage.value = qrURL ? await QRCode.toDataURL(qrURL) : ''
    if (!qrURL || !qrToken.value) throw new Error('二维码链接生成失败')
    const token = qrToken.value
    if (token) {
      qrTimer = setInterval(() => pollQR(token), 1000)
      qrExpiryTimer = setTimeout(() => {
        stopQRPolling()
        qrResult.value = { ...qrResult.value, status: 'expired' }
      }, 3 * 60 * 1000)
    }
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '登录二维码创建失败') } finally { securityLoading.value = false }
}
async function pollQR(token: string, manual = false) {
  if (!securityAccount.value || qrPolling.value || token !== qrToken.value) return
  qrPolling.value = true
  try {
    const account = securityAccount.value
    const result = await queryQQSecurityQRStatus(account.qq_number, token)
    if (token !== qrToken.value || !securityDialogVisible.value) return
    qrResult.value = { ...qrResult.value, ...result.data }
    const status = String(result.data?.status || '')
    if (status === 'confirmed') {
      stopQRPolling()
      const loginResult = result.data?.login_result as QQLoginResult | undefined
      if (loginResult) await handleLoginResult(account, loginResult)
      else ElMessage.warning('已扫码确认，但未收到登录结果，请重试登录')
    } else if (manual && status !== 'expired') {
      ElMessage.info(qrStatusLabel(status))
    }
  } catch (error) { console.warn('扫码状态查询失败', error); if (manual) ElMessage.error('二维码状态查询失败，请重试') }
  finally { qrPolling.value = false }
}
async function confirmSecurityScan() {
  if (!securityAccount.value || securityLoading.value) return
  securityLoading.value = true
  try {
    const account = securityAccount.value
    const result = await confirmQQSecurity(account.qq_number)
    if (result.data) await handleLoginResult(account, result.data)
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '继续 QQ 登录失败') }
  finally { securityLoading.value = false }
}
async function requestSMS(type: number) {
  if (!securityAccount.value) return
	const methods = securityMethodData().sms_phone as Record<string, unknown> | undefined
  const sign = String(methods?.sign || '')
  if (!sign) { ElMessage.warning('当前没有可用的短信验证签名'); return }
  securityLoading.value = true
  try {
    stopQRPolling()
    qrResult.value = null
    qrCodeImage.value = ''
    qrToken.value = ''
    const result = await getQQSecuritySMS(securityAccount.value.qq_number, type, sign)
    const data = result.data || {}
    if (securityResultState(data) !== 1) {
      throw new Error(findString(data, ['prompt']) || '短信验证请求未通过')
    }
    smsType.value = type
    smsSign.value = String(data.sign || sign)
    smsMessage.value = findString(data, ['sms', 'content', 'text', 'message'])
    smsTarget.value = type === 3 ? findString(data, ['send_to', 'sendTo', 'to']) : findString(data, ['masked_phone', 'phone'])
    if (type === 3) ElMessage.info('请按页面中的短信内容完成发送')
    else ElMessage.success(`短信已发送至 ${smsTarget.value || '密保手机'}`)
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '短信验证请求失败') } finally { securityLoading.value = false }
}
async function submitSMS() {
  if (!securityAccount.value || !smsType.value || !smsSign.value) return
  if (smsType.value === 4 && !smsCode.value.trim()) { ElMessage.warning('请输入短信验证码'); return }
  securityLoading.value = true
  try {
    const result = await checkQQSecuritySMS(securityAccount.value.qq_number, smsType.value, smsSign.value, smsCode.value)
    const data = result.data || {}
    if (!data.verified) {
      ElMessage.warning(findString(data.result, ['prompt']) || '短信验证尚未通过')
      return
    }
    const loginResult = data.login_result as QQLoginResult | undefined
    if (loginResult) await handleLoginResult(securityAccount.value, loginResult)
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '短信验证提交失败') } finally { securityLoading.value = false }
}
async function copySMSMessage() {
  if (!smsMessage.value) return
  try {
    await navigator.clipboard.writeText(smsMessage.value)
    ElMessage.success('短信内容已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选择短信内容')
  }
}
async function offlineAccount(account: UserQQAccount) {
  offlineQQLoading.value = account.qq_number
  try { await offlineQQ(account.qq_number); ElMessage.success(account.status === 2 ? 'QQ 登录已取消' : 'QQ 已下线'); await load() }
  catch (error) { ElMessage.error(error instanceof Error ? error.message : '停止 QQ 登录会话失败') }
  finally { offlineQQLoading.value = null }
}
async function editAccount(account: UserQQAccount) {
  editLoading.value = true
  try {
    const result = await getQQAccountOptions()
    editProtocols.value = result.data?.protocols || []
    editDeviceProfiles.value = result.data?.device_profiles || []
    editForm.value = { qq_number: account.qq_number, password: '', protocol_id: account.protocol_id ?? editProtocols.value[0]?.id ?? null, device_profile_id: account.device_profile_id ?? editDeviceProfiles.value[0]?.id ?? null, node_id: account.node_id || nodes.value.find(node => node.enabled)?.id || null }
    editDialogVisible.value = true
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '协议和设备指纹加载失败') } finally { editLoading.value = false }
}
async function submitEdit() {
  if (!editForm.value.qq_number || !editForm.value.password || editForm.value.protocol_id === null || editForm.value.device_profile_id === null || editForm.value.node_id === null) { ElMessage.warning('请完整填写 QQ 密码、登录节点、协议和设备指纹'); return }
  editLoading.value = true
  try {
    await updateQQ(editForm.value.qq_number, { password: editForm.value.password, protocol_id: editForm.value.protocol_id, device_profile_id: editForm.value.device_profile_id, node_id: editForm.value.node_id })
    ElMessage.success('QQ 账号修改成功')
    editDialogVisible.value = false
    await load()
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : 'QQ 账号修改失败') } finally { editLoading.value = false }
}
async function renewAccount(account: UserQQAccount) {
  renewAccountTarget.value = account
  renewPlanID.value = plans.value[0]?.id || 1
  renewDialogVisible.value = true
}
async function confirmRenew() {
  if (!renewAccountTarget.value) return
  if (walletCoins.value < renewTotalPrice.value) { ElMessage.warning(`当前金币不足，还需要 ${renewTotalPrice.value - walletCoins.value} 金币`); return }
  renewQQLoading.value = renewAccountTarget.value.qq_number
  try {
    await renewQQPlan(renewAccountTarget.value.qq_number, renewPlanID.value)
    ElMessage.success('QQ 续费成功')
    renewDialogVisible.value = false
    await load()
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : 'QQ 续费失败') } finally { renewQQLoading.value = null }
}

async function load() {
  loading.value = true
  try {
    const [result, walletResult, plansResult] = await Promise.all([getUserQQAccounts(), getWallet(), getQQPlans()])
    accounts.value = result.data || []
    walletCoins.value = walletResult.data?.coins || 0
    plans.value = plansResult.data || []
    if (!plans.value.some(plan => plan.id === addForm.value.plan_id)) addForm.value.plan_id = plans.value[0]?.id || 1
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : 'QQ 列表加载失败')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  window.addEventListener('message', handleSliderCaptchaMessage)
  load(); loadNodes(); loadOptions(); loadPrice()
})
onBeforeUnmount(() => {
  window.removeEventListener('message', handleSliderCaptchaMessage)
  stopQRPolling()
})
function closeSecurityDialog() {
  stopQRPolling()
  qrToken.value = ''
  sliderFrameURL.value = ''
  identityFrameURL.value = ''
}
function openSecurityURL(value: unknown) { if (typeof value === 'string' && /^https?:\/\//i.test(value)) window.open(value, '_blank', 'noopener,noreferrer') }
async function loadPrice() { try { const result = await getQQAccountPrice(); monthlyPrice.value = result.data?.monthly_price || 0 } catch (error) { ElMessage.error(error instanceof Error ? error.message : 'QQ 价格加载失败') } }
async function loadNodes() {
  optionsLoading.value = true
  try {
    const result = await getQQNodes()
    nodes.value = result.data || []
    const selected = nodes.value.find(node => node.id === addForm.value.node_id && node.enabled) || nodes.value.find(node => node.enabled)
    addForm.value.node_id = selected?.id ?? null
	} catch (error) { ElMessage.error(error instanceof Error ? error.message : '节点列表加载失败') } finally { optionsLoading.value = false }
}
async function loadOptions() {
  protocols.value = []
  deviceProfiles.value = []
  addForm.value.protocol_id = null
  addForm.value.device_profile_id = null
	optionsLoading.value = true
	try {
		const result = await getQQAccountOptions()
    protocols.value = result.data?.protocols || []
    deviceProfiles.value = result.data?.device_profiles || []
    if (protocols.value.length) addForm.value.protocol_id = protocols.value[0].id
    if (deviceProfiles.value.length) addForm.value.device_profile_id = deviceProfiles.value[0].id
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '协议和设备指纹加载失败') } finally { optionsLoading.value = false }
}
async function submitAdd() {
  if (!addForm.value.qq_number.trim() || !addForm.value.password || addForm.value.node_id === null || addForm.value.protocol_id === null || addForm.value.device_profile_id === null) { ElMessage.warning('请完整填写 QQ 号、密码、节点、协议和设备指纹'); return }
  addLoading.value = true
  try {
    await addQQ({ qq_number: addForm.value.qq_number.trim(), password: addForm.value.password, protocol_id: addForm.value.protocol_id, device_profile_id: addForm.value.device_profile_id, service_months: 1, plan_id: addForm.value.plan_id, node_id: addForm.value.node_id })
    ElMessage.success('QQ 添加成功，当前尚未登录')
    addForm.value = { qq_number: '', password: '', protocol_id: addForm.value.protocol_id, device_profile_id: addForm.value.device_profile_id, plan_id: addForm.value.plan_id, node_id: addForm.value.node_id }
    addDialogVisible.value = false
    await load()
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : 'QQ 添加失败') } finally { addLoading.value = false }
}
</script>

<template>
  <div class="page-heading">
    <div><h1>我的 QQ</h1></div>
    <el-button type="primary" @click="openAddDialog">添加账号</el-button>
  </div>
  <el-card v-loading="loading">
    <el-empty v-if="!accounts.length && !loading" description="当前没有已绑定的 QQ" />
    <el-table v-else :data="accounts" stripe>
      <el-table-column label="QQ 号" prop="qq_number" min-width="180" />
      <el-table-column label="绑定时间" min-width="200"><template #default="scope">{{ formatDateTime(scope.row.created_at) }}</template></el-table-column>
      <el-table-column label="服务期限" min-width="180"><template #default="scope">{{ formatDateTime(scope.row.service_expires_at) }}</template></el-table-column>
      <el-table-column label="QQ 名称" prop="nickname" min-width="160"><template #default="scope">{{ scope.row.nickname || '未获取' }}</template></el-table-column>
      <el-table-column label="所属节点" prop="node_name" min-width="140" />
      <el-table-column label="当前状态" min-width="120"><template #default="scope"><el-tag v-if="scope.row.expired" type="danger">已到期</el-tag><el-tag v-else :type="statusInfo(scope.row.status).type">{{ statusInfo(scope.row.status).label }}</el-tag></template></el-table-column>
      <el-table-column label="操作" min-width="330"><template #default="scope"><el-button link type="warning" :loading="renewQQLoading === scope.row.qq_number" @click="renewAccount(scope.row)">续费</el-button><el-button link type="primary" @click="editAccount(scope.row)">修改账号</el-button><el-button v-if="!scope.row.expired && scope.row.status === 0" link type="primary" :loading="loginQQLoading === scope.row.qq_number" @click="loginAccount(scope.row)">登录</el-button><el-button v-if="scope.row.status === 1" link type="warning" :loading="offlineQQLoading === scope.row.qq_number" @click="offlineAccount(scope.row)">下线</el-button><el-button v-if="scope.row.status === 2" link type="danger" :loading="offlineQQLoading === scope.row.qq_number" @click="offlineAccount(scope.row)">取消登录</el-button><el-button link type="primary" :loading="infoLoading" @click="showInfo(scope.row)">查看详情</el-button></template></el-table-column>
    </el-table>
    <div v-if="accounts.length" class="form-help">QQ 号不可修改；协议、设备指纹和密码可通过“修改账号”更新，账号必须先下线。</div>
  </el-card>
  <el-dialog v-model="infoDialogVisible" title="QQ 账号信息" width="620px">
    <el-descriptions v-if="selectedInfo" :column="2" border>
      <el-descriptions-item v-for="(value, key) in selectedInfo" :key="key" :label="String(key)">{{ typeof value === 'object' ? JSON.stringify(value) : String(value ?? '-') }}</el-descriptions-item>
    </el-descriptions>
    <el-empty v-else description="暂无账号信息" />
  </el-dialog>
  <el-dialog v-model="editDialogVisible" title="修改 QQ 账号" width="min(480px, calc(100vw - 32px))">
    <p class="form-help">QQ 号仅用于确认当前账号，不可修改。萌卡要求编辑时重新输入 QQ 密码，密码不会保存到本地系统。</p>
    <el-form label-width="90px" @submit.prevent="submitEdit">
      <el-form-item label="QQ 号"><el-input v-model="editForm.qq_number" disabled /></el-form-item>
      <el-form-item label="QQ 密码"><el-input v-model="editForm.password" type="password" show-password autocomplete="new-password" placeholder="请输入当前 QQ 密码" /></el-form-item>
      <el-form-item label="登录节点"><el-select v-model="editForm.node_id" placeholder="请选择萌卡登录节点" style="width: 100%"><el-option v-for="node in nodes" :key="node.id" :label="nodeLabel(node)" :value="node.id" :disabled="!node.enabled" /></el-select></el-form-item>
      <el-form-item label="协议"><el-select v-model="editForm.protocol_id" placeholder="请选择协议" style="width: 100%"><el-option v-for="item in editProtocols" :key="item.id" :label="item.name || `协议 ${item.id}`" :value="item.id" /></el-select></el-form-item>
      <el-form-item label="设备指纹"><el-select v-model="editForm.device_profile_id" placeholder="请选择设备指纹" style="width: 100%"><el-option v-for="item in editDeviceProfiles" :key="item.id" :label="item.name || `设备指纹 ${item.id}`" :value="item.id" /></el-select></el-form-item>
    </el-form>
    <template #footer><el-button @click="editDialogVisible = false">取消</el-button><el-button type="primary" :loading="editLoading" @click="submitEdit">保存修改</el-button></template>
  </el-dialog>
  <el-dialog v-model="securityDialogVisible" title="QQ 安全验证" width="min(560px, calc(100vw - 32px))" class="security-dialog" :teleported="true" :z-index="3000" @closed="closeSecurityDialog">
    <div class="security-panel" v-loading="securityLoading">
      <div class="security-intro">
        <el-avatar class="security-intro-icon" :size="42" :src="securityAccount ? qqAvatarUrl(securityAccount.qq_number) : ''">QQ</el-avatar>
        <div>
          <strong>完成验证后继续登录</strong>
          <span>当前账号：{{ securityAccount?.qq_number || '-' }}</span>
        </div>
      </div>
      <div v-if="securityResult" class="security-notice">
        <span class="security-notice-dot"></span>
        <div><strong>{{ securityResult.message }}</strong><span>请按页面提示完成当前验证</span></div>
      </div>
      <div v-if="securityResult?.code === 140022008" class="slider-panel security-stage">
        <div class="security-stage-heading"><div><strong>滑块验证</strong><span>请在下方完成验证</span></div></div>
        <div class="slider-stage-body" v-loading="sliderLoading">
          <iframe v-if="sliderFrameURL" class="security-slider-frame" :src="sliderFrameURL" title="QQ 滑块验证" />
        </div>
      </div>
      <div v-else-if="securityResult?.code === 140022007" class="security-stage identity-stage">
        <div class="security-stage-heading"><div><strong>身份验证</strong><span>请按页面要求完成身份验证</span></div></div>
        <div class="identity-content">
          <div v-if="identityStage === 'captcha'" class="embedded-security-page">
            <iframe v-if="identityFrameURL" :src="identityFrameURL" class="security-slider-frame" title="QQ 身份验证" />
            <p v-else class="form-help">身份验证地址不可用，请取消登录后重试。</p>
          </div>
          <div v-else-if="identityStage === 'phone'" class="security-sms-form">
            <p>请输入该 QQ 绑定的手机号，系统会返回需要发送的验证短信。</p>
            <p v-if="identityMaskedMobile"><strong>{{ identityMaskedMobile }}</strong></p>
            <div class="identity-phone-row">
              <el-input v-model="identityAreaCode" disabled style="width: 90px" />
              <el-input v-model="identityMobile" inputmode="tel" autocomplete="tel" placeholder="绑定手机号" />
            </div>
            <el-button type="primary" :loading="securityLoading" :disabled="!identityMobile.trim()" @click="requestIdentitySMS">获取短信</el-button>
          </div>
          <div v-else class="security-sms-form">
            <p>请使用绑定手机按以下内容发送短信，发送完成后点击确认。</p>
            <div class="sms-instruction">
              <span>收件人</span><strong class="sms-target">{{ identitySMSTarget || '指定号码' }}</strong>
              <span>短信内容</span><code class="sms-content">{{ identitySMSContent || '请按手机提示完成发送' }}</code>
              <el-button v-if="identitySMSContent" size="small" @click="copyIdentitySMSMessage">复制短信内容</el-button>
            </div>
            <el-button type="primary" :loading="securityLoading" @click="confirmIdentitySMS">我已发送</el-button>
          </div>
          <el-descriptions :column="1" border>
            <el-descriptions-item label="当前协议">{{ protocolName(securityAccount?.protocol_id) }}</el-descriptions-item>
            <el-descriptions-item label="当前设备指纹">{{ deviceProfileName(securityAccount?.device_profile_id) }}</el-descriptions-item>
          </el-descriptions>
          <p class="form-help">身份验证票据仅用于当前登录会话，关闭后需重新发起登录。</p>
        </div>
      </div>
      <template v-else>
        <div v-if="isEmbeddedQQSecurityURL(securityURL)" class="security-result security-stage embedded-security-page">
          <div class="security-stage-heading"><div><strong>QQ 安全验证</strong><span>请直接在下方页面完成验证</span></div></div>
          <iframe :src="securityURL" class="security-page-frame" title="QQ 安全验证页面" allow="clipboard-read; clipboard-write" />
          <p class="form-help">完成验证后请返回此窗口，QQ 登录状态会在验证结果同步后更新。</p>
        </div>
        <div v-else-if="securityQRCode" class="security-result security-stage">
          <div class="security-stage-heading"><div><strong>扫码验证</strong><span>请使用 QQ 扫描二维码完成验证</span></div></div>
          <img :src="securityQRCode" alt="QQ 安全验证二维码" style="display: block; width: 200px; height: 200px; margin: 16px auto 0" />
          <el-button type="primary" :loading="securityLoading" @click="confirmSecurityScan">已完成扫码，继续登录</el-button>
        </div>
        <div class="security-stage-heading"><div><strong>可用验证方式</strong><span>{{ String(securityMethods?.prompt || '请选择一种验证方式') }}</span></div></div>
        <div class="security-actions">
          <button v-for="method in availableVerifyMethods()" :key="method.type" class="security-method" :class="{ 'security-method-primary': method.type === 10 }" :disabled="method.disabled" type="button" @click="method.action"><span class="security-method-icon">{{ method.type === 10 ? '码' : '信' }}</span><span><strong>{{ method.label }}</strong><small>{{ method.description }}</small></span><span class="security-method-arrow">›</span></button>
        </div>
        <el-button class="security-refresh" text @click="refreshSecurityMethods">刷新验证方式</el-button>
        <div v-if="qrResult" class="security-result security-stage">
          <div class="security-stage-heading"><div><strong>扫码验证</strong><span>{{ qrStatusLabel(qrResult.status) }}</span></div></div>
          <img v-if="qrCodeImage" :src="qrCodeImage" alt="QQ 登录二维码" class="security-qr-image" />
          <el-button v-if="qrResult.qr_url" type="primary" plain @click="openSecurityURL(qrResult.qr_url)">打开登录二维码</el-button>
          <el-button v-if="qrResult.status !== 'expired'" :loading="qrPolling" @click="pollQR(qrToken, true)">已完成扫码，检查结果</el-button>
        </div>
        <div v-if="smsType && smsSign" class="security-sms-form security-stage">
          <div class="security-stage-heading"><div><strong>{{ smsType === 4 ? '接收短信' : '发送短信' }}</strong><span>{{ smsType === 4 ? '输入密保手机收到的验证码' : '请按以下内容完成短信发送' }}</span></div></div>
          <template v-if="smsType === 4">
            <p>验证码已发送至 {{ smsTarget || '密保手机' }}。</p>
            <el-input v-model="smsCode" clearable maxlength="12" placeholder="请输入短信验证码" />
          </template>
          <div v-else class="sms-instruction">
            <p>请使用密保手机将以下内容原样发送到：</p>
            <strong class="sms-target">{{ smsTarget || '指定号码' }}</strong>
            <code class="sms-content">{{ smsMessage || '萌卡未返回短信内容，请刷新后重试' }}</code>
            <el-button v-if="smsMessage" size="small" @click="copySMSMessage">复制短信内容</el-button>
            <p>发送完成后，点击下方按钮检查验证结果。</p>
          </div>
          <el-button type="primary" :loading="securityLoading" :disabled="smsType === 4 && !smsCode.trim()" @click="submitSMS">提交验证结果</el-button>
        </div>
      </template>
    </div>
  </el-dialog>
  <el-dialog v-model="renewDialogVisible" title="购买 QQ 套餐" width="min(480px, calc(100vw - 32px))">
    <div class="qq-renew-summary">
      <div><span>当前金币</span><strong>{{ walletCoins }}</strong></div>
      <div><span>选择套餐</span><el-select v-model="renewPlanID" style="width: 220px"><el-option v-for="plan in plans" :key="plan.id" :label="`${plan.name} · ${plan.price} 金币 / ${plan.service_days} 天`" :value="plan.id" /></el-select></div>
      <div v-if="selectedRenewPlan" class="form-help">{{ selectedRenewPlan.description || `购买后增加 ${selectedRenewPlan.service_days} 天服务` }}</div>
      <div class="qq-renew-total"><span>总金额</span><strong>{{ renewTotalPrice }} 金币</strong></div>
    </div>
    <template #footer><el-button @click="renewDialogVisible = false">取消</el-button><el-button type="primary" :loading="Boolean(renewQQLoading)" @click="confirmRenew">确认续费</el-button></template>
  </el-dialog>
  <el-dialog v-model="addDialogVisible" title="添加 QQ 账号" width="min(520px, calc(100vw - 32px))" :close-on-click-modal="!addLoading" :close-on-press-escape="!addLoading" :show-close="!addLoading">
    <div v-loading="optionsLoading">
    <p class="form-help qq-login-help">QQ 号提交后不可修改，协议、设备指纹和密码可通过账号列表中的“修改账号”更新。密码仅用于本次请求，不会保存到本地系统。</p>
    <el-form label-width="90px" @submit.prevent="submitAdd">
      <el-form-item label="QQ 号"><el-input v-model="addForm.qq_number" inputmode="numeric" maxlength="10" placeholder="请输入 QQ 号" /></el-form-item>
      <el-form-item label="QQ 密码"><el-input v-model="addForm.password" type="password" show-password autocomplete="off" placeholder="请输入 QQ 密码" /></el-form-item>
      <el-form-item label="登录节点"><el-select v-model="addForm.node_id" placeholder="请选择萌卡登录节点" style="width: 100%"><el-option v-for="node in nodes" :key="node.id" :label="nodeLabel(node)" :value="node.id" :disabled="!node.enabled" /></el-select></el-form-item>
      <el-form-item label="协议"><el-select v-model="addForm.protocol_id" placeholder="请选择协议" style="width: 100%"><el-option v-for="item in protocols" :key="item.id" :label="item.name || `协议 ${item.id}`" :value="item.id" /></el-select></el-form-item>
      <el-form-item label="设备指纹"><el-select v-model="addForm.device_profile_id" placeholder="请选择设备指纹" style="width: 100%"><el-option v-for="item in deviceProfiles" :key="item.id" :label="item.name || `设备指纹 ${item.id}`" :value="item.id" /></el-select></el-form-item>
      <el-form-item label="购买套餐"><el-select v-model="addForm.plan_id" style="width: 100%"><el-option v-for="plan in plans" :key="plan.id" :label="`${plan.name} · ${plan.price} 金币 / ${plan.service_days} 天`" :value="plan.id" /></el-select><div v-if="selectedAddPlan" class="form-help">{{ selectedAddPlan.description || `购买后获得 ${selectedAddPlan.service_days} 天服务` }}，共需 {{ totalPrice }} 金币。</div></el-form-item>
    </el-form>
    </div>
    <template #footer><el-button :disabled="addLoading" @click="addDialogVisible = false">取消</el-button><el-button type="primary" :loading="addLoading" :disabled="optionsLoading" @click="submitAdd">添加账号</el-button></template>
  </el-dialog>
</template>
