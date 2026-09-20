<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { getPetAccounts, getPetPKStatus, getPetPKStrangers, settlePetPK, startPetPK, type PetAccount, type PetPKStartResult, type PetPKStatusResult, type PetPKStranger } from '../../../api/pet'

const route = useRoute(); const router = useRouter()
const accounts = ref<PetAccount[]>([]); const activeQQ = ref(String(route.query.qq || '')); const items = ref<PetPKStranger[]>([]); const loading = ref(true); const pkLoading = ref(false); const settling = ref(false); const visible = ref(false); const start = ref<PetPKStartResult | null>(null); const status = ref<PetPKStatusResult | null>(null)
const page = ref(1); const pageSize = ref(10); const pageItems = computed(() => items.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value))
async function load() { loading.value = true; try { accounts.value = (await getPetAccounts()).data || []; if (!activeQQ.value) activeQQ.value = accounts.value.find(x => !x.expired)?.qq_number || ''; if (activeQQ.value) items.value = (await getPetPKStrangers(activeQQ.value)).data || [] } catch (e) { ElMessage.error(e instanceof Error ? e.message : '陌生人数据加载失败') } finally { loading.value = false } }
async function selectQQ(qq: string) { activeQQ.value = qq; page.value = 1; items.value = (await getPetPKStrangers(qq)).data || [] }
async function startPK(item: PetPKStranger) { pkLoading.value = true; try { start.value = (await startPetPK(activeQQ.value, item.user_id, item.pet_id)).data; if (!start.value?.story_id) throw new Error('开始 PK 未返回 story_id'); visible.value = true; status.value = (await getPetPKStatus(activeQQ.value, start.value.story_id)).data } catch (e) { ElMessage.error(e instanceof Error ? e.message : '开始 PK 失败') } finally { pkLoading.value = false } }
async function refreshStatus() { if (!start.value?.story_id) return; pkLoading.value = true; try { status.value = (await getPetPKStatus(activeQQ.value, start.value.story_id)).data } finally { pkLoading.value = false } }
async function settle() { if (!start.value?.story_id) return; settling.value = true; try { status.value = (await settlePetPK(activeQQ.value, start.value.story_id)).data; ElMessage.success('PK 结算成功') } catch (e) { ElMessage.error(e instanceof Error ? e.message : 'PK 结算失败') } finally { settling.value = false } }
function typeLabel(value: number, updated: boolean) { if (!updated) return '未获取'; return ({ 1: '智力型', 2: '力量型', 3: '魅力型', 10: '综合型' } as Record<number, string>)[value] || String(value) }
onMounted(() => void load())
</script>
<template>
  <div class="page-heading"><div><h1>手动PK</h1><p>选择陌生人宠物发起手动 PK。</p></div><div><el-button @click="load">刷新</el-button><el-button @click="router.push('/pet')">返回宠物</el-button></div></div>
  <el-card v-loading="loading"><div class="pet-account-switcher"><button v-for="account in accounts" :key="account.qq_number" type="button" class="pet-account-button" :class="{ 'is-active': activeQQ === account.qq_number }" @click="selectQQ(account.qq_number)"><span class="pet-account-button-name">{{ account.nickname || account.qq_number }}</span><span class="pet-account-button-qq">QQ {{ account.qq_number }}</span></button></div>
    <el-table :data="pageItems" stripe border><el-table-column prop="pet_name" label="宠物名称" width="120" /><el-table-column prop="nickname" label="昵称" width="150" /><el-table-column prop="user_id" label="用户 ID" width="120" /><el-table-column label="宠物类型" width="110"><template #default="s">{{ typeLabel(s.row.dominant_type, Boolean(s.row.power_updated_at)) }}</template></el-table-column><el-table-column prop="power" label="战力" width="90" /><el-table-column label="操作" min-width="140"><template #default="s"><el-button type="danger" link :loading="pkLoading" @click="startPK(s.row)">手动 PK</el-button></template></el-table-column></el-table>
    <div class="pet-pk-pagination"><span>共 {{ items.length }} 条</span><el-pagination v-model:current-page="page" :page-size="pageSize" :total="items.length" layout="prev, pager, next, jumper" /></div>
  </el-card>
  <el-dialog v-model="visible" title="手动 PK" width="520px"><el-descriptions v-if="start" :column="1" border><el-descriptions-item label="对手">{{ start.friend_pet_name || start.friend_id }}</el-descriptions-item><el-descriptions-item label="Story ID">{{ start.story_id }}</el-descriptions-item><el-descriptions-item label="响应为空">{{ status?.response_empty ? '是' : '否' }}</el-descriptions-item></el-descriptions><template #footer><el-button :loading="pkLoading" @click="refreshStatus">刷新状态</el-button><el-button type="primary" :loading="settling" :disabled="!status?.response_empty" @click="settle">结算 PK</el-button></template></el-dialog>
</template>
