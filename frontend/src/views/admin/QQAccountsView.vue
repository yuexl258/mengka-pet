<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { deleteLocalQQ, getAdminQQPlans, getMokantBots, getUsers, updateQQBinding, type AdminQQPlan, type AdminUser, type MokantBot } from '../../api/admin'
import { formatDateTime } from '../../utils/format'

const loading = ref(false)
const bots = ref<MokantBot[]>([])
const users = ref<AdminUser[]>([])
const dialogVisible = ref(false)
const selectedQQ = ref<number | null>(null)
const selectedUserID = ref<number | null>(null)
const binding = ref(false)
const plans = ref<AdminQQPlan[]>([])
const selectedPlanID = ref<number | null>(null)

const statusMap: Record<number, { label: string; type: 'info' | 'success' | 'warning' }> = {
  0: { label: '离线', type: 'info' },
  1: { label: '在线', type: 'success' },
  2: { label: '登录中', type: 'warning' },
}

function statusInfo(status: number) { return statusMap[status] || { label: '未知', type: 'info' as const } }
function sourceInfo(bot: MokantBot) {
  if (!bot.framework_checked) return { label: '框架状态未知', type: 'warning' as const }
  if (bot.framework_present && bot.database_present) return { label: '框架 / 本地', type: 'success' as const }
  if (bot.framework_present) return { label: '仅框架', type: 'info' as const }
  return { label: '仅本地', type: 'danger' as const }
}
function openBinding(bot: MokantBot) { selectedQQ.value = bot.self_id; selectedUserID.value = bot.bound_user_id; selectedPlanID.value = plans.value.find(plan => plan.status === 'active')?.id || null; dialogVisible.value = true }
async function loadUsers() {
  const result = await getUsers({ page: 1, page_size: 100, status: 'active' })
  users.value = result.data?.items || []
}
async function loadPlans() {
  const result = await getAdminQQPlans()
  plans.value = result.data || []
}
async function load() {
  loading.value = true
  try {
    const result = await getMokantBots()
    bots.value = result.data || []
    await Promise.all([loadUsers(), loadPlans()])
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : 'QQ 列表加载失败')
  } finally { loading.value = false }
}
async function saveBinding() {
  if (!selectedQQ.value || !selectedUserID.value || !selectedPlanID.value) {
    ElMessage.warning('请选择用户和套餐')
    return
  }
  binding.value = true
  try {
    const result = await updateQQBinding(selectedQQ.value, selectedUserID.value, selectedPlanID.value)
    ElMessage.success(result.data?.service_expires_at ? `QQ 绑定已更新，到期时间：${result.data.service_expires_at}` : 'QQ 绑定已更新')
    dialogVisible.value = false
    await load()
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '绑定失败') } finally { binding.value = false }
}
async function removeLocalQQ(bot: MokantBot) {
  try {
    await ElMessageBox.confirm(
      `QQ ${bot.self_id} 未被框架返回。删除后，本地绑定、宠物资料、好友缓存和自动任务数据将一并删除，确定继续吗？`,
      '删除本地 QQ',
      { type: 'warning', confirmButtonText: '确认删除' },
    )
    await deleteLocalQQ(bot.self_id)
    ElMessage.success('本地 QQ 记录及关联数据已删除')
    await load()
  } catch (error) { if (error !== 'cancel') ElMessage.error(error instanceof Error ? error.message : '删除失败') }
}

onMounted(load)
</script>

<template>
  <div class="page-heading"><div><h1>QQ 管理</h1></div></div>
  <el-card v-loading="loading">
    <el-empty v-if="!bots.length && !loading" description="暂无 QQ 机器人，或萌卡 NT 尚未返回列表" />
    <el-table v-else class="qq-accounts-table" :data="bots" stripe>
      <el-table-column label="QQ 号" prop="self_id" width="145" />
      <el-table-column label="数据来源" width="130"><template #default="scope"><el-tag :type="sourceInfo(scope.row).type">{{ sourceInfo(scope.row).label }}</el-tag></template></el-table-column>
      <el-table-column label="昵称" min-width="120"><template #default="scope">{{ scope.row.framework_present ? (scope.row.nickname || '-') : '-' }}</template></el-table-column>
      <el-table-column label="状态" width="105"><template #default="scope"><el-tag v-if="scope.row.framework_present" :type="statusInfo(scope.row.status).type">{{ statusInfo(scope.row.status).label }}</el-tag><el-tag v-else type="info">未返回</el-tag></template></el-table-column>
      <el-table-column label="绑定用户" min-width="150"><template #default="scope">{{ scope.row.bound_username || '未绑定' }}</template></el-table-column>
      <el-table-column label="到期时间" min-width="170"><template #default="scope">{{ formatDateTime(scope.row.service_expires_at) }}</template></el-table-column>
      <el-table-column label="操作" fixed="right" width="180"><template #default="scope"><el-button v-if="scope.row.framework_present" link type="primary" @click="openBinding(scope.row)">{{ scope.row.bound_user_id ? '修改绑定' : '绑定用户' }}</el-button><el-button v-if="scope.row.database_present && scope.row.framework_checked && !scope.row.framework_present" link type="danger" @click="removeLocalQQ(scope.row)">删除</el-button></template></el-table-column>
    </el-table>
  </el-card>
  <el-dialog v-model="dialogVisible" title="绑定 QQ 用户" width="420px">
    <el-form label-width="80px"><el-form-item label="QQ 号"><span>{{ selectedQQ }}</span></el-form-item><el-form-item label="本地用户"><el-select v-model="selectedUserID" filterable placeholder="请选择用户" style="width: 100%"><el-option v-for="user in users" :key="user.id" :label="`${user.username}（ID: ${user.id}）`" :value="user.id" /></el-select></el-form-item><el-form-item label="QQ 套餐"><el-select v-model="selectedPlanID" placeholder="请选择套餐" style="width: 100%"><el-option v-for="plan in plans.filter(item => item.status === 'active')" :key="plan.id" :label="`${plan.name}（${plan.price} 金币 / ${plan.service_days} 天）`" :value="plan.id" /></el-select></el-form-item></el-form>
    <template #footer><el-button @click="dialogVisible = false">取消</el-button><el-button type="primary" :loading="binding" :disabled="!selectedUserID || !selectedPlanID" @click="saveBinding">保存绑定</el-button></template>
  </el-dialog>
</template>
