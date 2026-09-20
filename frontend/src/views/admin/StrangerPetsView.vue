<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { deleteStrangerPet, getStrangerPet, getStrangerPets, importStrangerPet, importStrangerTXT, type StrangerPet } from '../../api/admin'
import { downloadStrangerTXT } from '../../api/client'
import { formatDateTime } from '../../utils/format'

const loading = ref(false)
const page = ref(1)
const total = ref(0)
const items = ref<StrangerPet[]>([])
const detail = ref<StrangerPet | null>(null)
const detailVisible = ref(false)
const importVisible = ref(false)
const importing = ref(false)
const importForm = reactive<{ user_id: string; pet_id: string; pet_name: string; nickname: string; power: number | undefined; dominant_type: number | undefined; pet_resolved: boolean }>({ user_id: '', pet_id: '', pet_name: '', nickname: '', power: undefined, dominant_type: undefined, pet_resolved: true })
const fileInput = ref<HTMLInputElement | null>(null)

async function load() {
  loading.value = true
  try {
    const result = await getStrangerPets(page.value, 20)
    items.value = result.data?.items || []
    total.value = result.data?.total || 0
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '陌生人数据加载失败') } finally { loading.value = false }
}
async function view(item: StrangerPet) {
  try { detail.value = (await getStrangerPet(item.id)).data; detailVisible.value = true } catch (error) { ElMessage.error(error instanceof Error ? error.message : '详情加载失败') }
}
async function remove(item: StrangerPet) {
  try { await ElMessageBox.confirm(`确认删除 user_id 为“${item.user_id}”的陌生人宠物？`, '删除确认'); await deleteStrangerPet(item.id); ElMessage.success('已删除'); await load() } catch (error) { if (error !== 'cancel') ElMessage.error(error instanceof Error ? error.message : '删除失败') }
}
function resetImportForm() {
  Object.assign(importForm, { user_id: '', pet_id: '', pet_name: '', nickname: '', power: undefined, dominant_type: undefined, pet_resolved: true })
}
async function saveImport() {
  if (!importForm.user_id.trim() || !importForm.pet_id.trim()) {
    ElMessage.warning('user_id 和宠物 ID 不能为空')
    return
  }
  importing.value = true
  try {
    const data: Record<string, unknown> = { ...importForm, user_id: importForm.user_id.trim(), pet_id: importForm.pet_id.trim(), power: importForm.power ?? null, dominant_type: importForm.dominant_type ?? null }
    await importStrangerPet(data)
    ElMessage.success('导入成功')
    importVisible.value = false
    resetImportForm()
    await load()
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '导入失败') } finally { importing.value = false }
}
async function chooseFile() { fileInput.value?.click() }
async function onFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file) return
  try { await importStrangerTXT(await file.text()); ElMessage.success('TXT 导入成功'); await load() } catch (error) { ElMessage.error(error instanceof Error ? error.message : 'TXT 导入失败') } finally { (event.target as HTMLInputElement).value = '' }
}
async function exportText(all: boolean) {
  try { await downloadStrangerTXT(page.value, 20, all); ElMessage.success(all ? '已导出全部列表' : '已导出当前列表') } catch (error) { ElMessage.error(error instanceof Error ? error.message : '导出失败') }
}
onMounted(load)
</script>

<template>
  <div class="page-heading"><div><h1>陌生人管理</h1><p>全局唯一保存陌生人宠物数据，不区分来源 QQ。</p></div><div class="page-actions"><el-button @click="importVisible = true">单条导入</el-button><el-button @click="chooseFile">导入 TXT</el-button><el-button @click="exportText(false)">导出当前页</el-button><el-button type="primary" @click="exportText(true)">导出全部</el-button><input ref="fileInput" type="file" accept=".txt,text/plain" hidden @change="onFile"></div></div>
  <el-card><el-table v-loading="loading" :data="items" empty-text="暂无陌生人宠物数据"><el-table-column prop="id" label="ID" width="80" /><el-table-column prop="user_id" label="陌生人 user_id" min-width="160" /><el-table-column prop="pet_id" label="宠物 ID" min-width="140" /><el-table-column prop="pet_name" label="宠物名称" min-width="120" /><el-table-column prop="nickname" label="昵称" min-width="120" /><el-table-column prop="power" label="战力" width="100" /><el-table-column label="更新时间" min-width="180"><template #default="scope">{{ formatDateTime(scope.row.updated_at) }}</template></el-table-column><el-table-column label="操作" fixed="right" width="150"><template #default="scope"><el-button link type="primary" @click="view(scope.row)">查看详情</el-button><el-button link type="danger" @click="remove(scope.row)">删除</el-button></template></el-table-column></el-table><div class="table-pagination"><el-pagination v-model:current-page="page" background layout="prev, pager, next" :page-size="20" :total="total" @current-change="load" /></div></el-card>
  <el-dialog v-model="detailVisible" title="陌生人宠物详情" width="680px"><el-descriptions :column="2" border><el-descriptions-item label="user_id">{{ detail?.user_id }}</el-descriptions-item><el-descriptions-item label="pet_id">{{ detail?.pet_id }}</el-descriptions-item><el-descriptions-item label="宠物名称">{{ detail?.pet_name }}</el-descriptions-item><el-descriptions-item label="昵称">{{ detail?.nickname }}</el-descriptions-item><el-descriptions-item label="战力">{{ detail?.power ?? '-' }}</el-descriptions-item><el-descriptions-item label="类型">{{ detail?.dominant_type ?? '-' }}</el-descriptions-item></el-descriptions><pre class="json-preview">{{ JSON.stringify(detail?.raw_data || {}, null, 2) }}</pre></el-dialog>
  <el-dialog v-model="importVisible" title="单条导入" width="600px" @closed="resetImportForm"><el-form label-width="110px"><el-form-item label="用户 ID" required><el-input v-model="importForm.user_id" placeholder="请输入陌生人 user_id" /></el-form-item><el-form-item label="宠物 ID" required><el-input v-model="importForm.pet_id" placeholder="请输入宠物 ID" /></el-form-item><el-form-item label="宠物名称"><el-input v-model="importForm.pet_name" /></el-form-item><el-form-item label="昵称"><el-input v-model="importForm.nickname" /></el-form-item><el-form-item label="战力"><el-input-number v-model="importForm.power" :min="0" :controls="false" placeholder="可不填" /></el-form-item><el-form-item label="宠物类型"><el-select v-model="importForm.dominant_type" clearable placeholder="可不填"><el-option label="智力型" value="1" /><el-option label="力量型" value="2" /><el-option label="魅力型" value="3" /><el-option label="综合型" value="10" /></el-select></el-form-item><el-form-item label="宠物已解析"><el-switch v-model="importForm.pet_resolved" /></el-form-item></el-form><template #footer><el-button @click="importVisible = false">取消</el-button><el-button type="primary" :loading="importing" @click="saveImport">导入</el-button></template></el-dialog>
</template>
