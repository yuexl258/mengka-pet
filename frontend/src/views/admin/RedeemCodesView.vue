<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { getRedeemCodeStats, getRedeemCodes, type RedeemCodeItem, type RedeemCodeStats } from '../../api/admin'
import { formatDateTime } from '../../utils/format'
import { generateCodes, type GenerateCodesResult } from '../../api/wallet'

const loading = ref(false)
const items = ref<RedeemCodeItem[]>([])
const stats = ref<RedeemCodeStats>({ total: 0, unused: 0, redeemed: 0 })
const total = ref(0)
const page = ref(1)
const status = ref('')
const codeForm = ref({ amount: 100, quantity: 1 }); const codeLoading = ref(false); const generated = ref<GenerateCodesResult | null>(null)
async function createCodes() { if (!Number.isInteger(codeForm.value.amount) || codeForm.value.amount < 1 || !Number.isInteger(codeForm.value.quantity) || codeForm.value.quantity < 1 || codeForm.value.quantity > 1000) { ElMessage.warning('请输入合法的正整数面额和生成数量'); return }; codeLoading.value = true; try { generated.value = (await generateCodes(codeForm.value.amount, codeForm.value.quantity)).data; ElMessage.success('卡密生成成功，请及时复制保存'); await load() } catch (error) { ElMessage.error(error instanceof Error ? error.message : '卡密生成失败') } finally { codeLoading.value = false } }
async function copyCodes() { if (!generated.value) return; await navigator.clipboard.writeText(generated.value.codes.join('\n')); ElMessage.success('卡密已复制') }
function downloadFile(filename: string, content: string, type: string) { const blob = new Blob([content], { type }); const url = URL.createObjectURL(blob); const link = document.createElement('a'); link.href = url; link.download = filename; link.click(); URL.revokeObjectURL(url) }
function csvValue(value: unknown) { return `"${String(value ?? '').replaceAll('"', '""')}"` }
function exportGenerated() { if (!generated.value) return; downloadFile('qq-pet-cards.txt', generated.value.codes.join('\r\n'), 'text/plain;charset=utf-8') }
function exportItems() { const header = ['编号', '卡密', '面额', '状态', '生成管理员', '生成时间', '使用用户', '使用时间']; const rows = items.value.map(item => [item.id, item.code || '历史卡密不可恢复', item.amount, item.status === 'redeemed' ? '已使用' : '未使用', item.created_by_name, item.created_at, item.redeemed_by_name || '', item.redeemed_at || '']); downloadFile('qq-pet-card-records.csv', '\ufeff' + [header, ...rows].map(row => row.map(csvValue).join(',')).join('\r\n'), 'text/csv;charset=utf-8') }

async function load() {
  loading.value = true
  try {
    const [list, summary] = await Promise.all([getRedeemCodes(page.value, 20, status.value), getRedeemCodeStats()])
    items.value = list.data?.items || []
    total.value = list.data?.total || 0
    stats.value = summary.data || stats.value
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '卡密记录加载失败')
  } finally {
    loading.value = false
  }
}

function changeStatus(value: string) {
  status.value = value
  page.value = 1
  load()
}

onMounted(load)
</script>

<template>
  <div class="page-heading"><div><h1>卡密管理</h1></div></div>
  <el-card class="redeem-generator"><template #header>生成卡密</template><el-form inline><el-form-item label="金币面额"><el-input-number v-model="codeForm.amount" :min="1" :step="1" step-strictly /></el-form-item><el-form-item label="生成数量"><el-input-number v-model="codeForm.quantity" :min="1" :max="1000" :step="1" step-strictly /></el-form-item><el-button type="primary" :loading="codeLoading" @click="createCodes">生成卡密</el-button></el-form><el-alert v-if="generated" title="卡密已加密保存，可在下方列表查看并导出。" type="warning" show-icon /><el-input v-if="generated" class="code-result" :model-value="generated.codes.join('\n')" type="textarea" :rows="Math.min(generated.codes.length + 1, 12)" readonly /><el-button v-if="generated" @click="copyCodes">复制全部卡密</el-button><el-button v-if="generated" @click="exportGenerated">导出 TXT</el-button></el-card>
  <el-row :gutter="16" class="equal-row stats-row">
    <el-col :xs="24" :sm="8"><el-card><el-statistic title="卡密总数" :value="stats.total" /></el-card></el-col>
    <el-col :xs="24" :sm="8"><el-card><el-statistic title="未使用" :value="stats.unused" /></el-card></el-col>
    <el-col :xs="24" :sm="8"><el-card><el-statistic title="已使用" :value="stats.redeemed" /></el-card></el-col>
  </el-row>
  <el-card class="codes-table-card">
    <div class="toolbar-row"><span class="section-title">卡密记录</span><el-radio-group :model-value="status" @change="changeStatus"><el-radio-button label="">全部</el-radio-button><el-radio-button label="unused">未使用</el-radio-button><el-radio-button label="redeemed">已使用</el-radio-button></el-radio-group><el-button @click="exportItems">导出当前页 CSV</el-button></div>
    <el-table v-loading="loading" :data="items" empty-text="暂无卡密记录">
      <el-table-column prop="id" label="编号" width="80" />
      <el-table-column label="具体卡密" min-width="250"><template #default="scope"><span class="redeem-code-value">{{ scope.row.code || '历史卡密不可恢复' }}</span></template></el-table-column>
      <el-table-column prop="amount" label="面额" width="100" />
      <el-table-column label="状态" width="110"><template #default="scope"><el-tag :type="scope.row.status === 'redeemed' ? 'success' : 'warning'">{{ scope.row.status === 'redeemed' ? '已使用' : '未使用' }}</el-tag></template></el-table-column>
      <el-table-column prop="created_by_name" label="生成管理员" min-width="130" />
      <el-table-column label="生成时间" min-width="180"><template #default="scope">{{ formatDateTime(scope.row.created_at) }}</template></el-table-column>
      <el-table-column label="使用用户" min-width="130"><template #default="scope">{{ scope.row.redeemed_by_name || '-' }}</template></el-table-column>
      <el-table-column label="使用时间" min-width="180"><template #default="scope">{{ formatDateTime(scope.row.redeemed_at) }}</template></el-table-column>
    </el-table>
    <div class="table-pagination"><el-pagination v-model:current-page="page" background layout="prev, pager, next" :page-size="20" :total="total" @current-change="load" /></div>
  </el-card>
</template>
