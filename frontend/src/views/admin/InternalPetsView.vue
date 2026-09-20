<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { createInternalPet, deleteInternalPet, getInternalPets, type InternalPet } from '../../api/admin'

const loading = ref(false)
const saving = ref(false)
const items = ref<InternalPet[]>([])
const form = reactive({ user_id: '', pet_id: '' })

async function load() {
  loading.value = true
  try { items.value = (await getInternalPets()).data || [] } catch (error) { ElMessage.error(error instanceof Error ? error.message : '内部宠物读取失败') } finally { loading.value = false }
}
async function add() {
  const userID = form.user_id.trim()
  const petID = form.pet_id.trim()
  if (!userID || !petID) return ElMessage.warning('请输入 QQ 和 pet_id')
  saving.value = true
  try { await createInternalPet({ user_id: userID, pet_id: petID }); form.user_id = ''; form.pet_id = ''; ElMessage.success('内部宠物已添加'); await load() } catch (error) { ElMessage.error(error instanceof Error ? error.message : '内部宠物添加失败') } finally { saving.value = false }
}
async function remove(item: InternalPet) {
  try { await ElMessageBox.confirm(`确认删除 QQ ${item.user_id} 的内部宠物？`, '删除确认'); await deleteInternalPet(item.id); ElMessage.success('已删除'); await load() } catch (error) { if (error !== 'cancel') ElMessage.error(error instanceof Error ? error.message : '删除失败') }
}
onMounted(load)
</script>

<template>
  <div class="page-heading"><div><h1>内部宠物</h1><p>自动 PK 每场正式 PK 前，会按列表顺序先与一只内部宠物 PK；内部 PK 不计入次数，也不写入日志。</p></div></div>
  <el-card class="internal-pet-form-card"><el-form inline @submit.prevent="add"><el-form-item label="QQ"><el-input v-model="form.user_id" placeholder="输入 QQ" /></el-form-item><el-form-item label="pet_id"><el-input v-model="form.pet_id" placeholder="输入 pet_id" /></el-form-item><el-form-item><el-button type="primary" :loading="saving" @click="add">添加</el-button></el-form-item></el-form></el-card>
  <el-card><el-table v-loading="loading" :data="items" empty-text="暂无内部宠物"><el-table-column type="index" label="#" width="70" /><el-table-column prop="user_id" label="QQ" min-width="180" /><el-table-column prop="pet_id" label="pet_id" min-width="240" /><el-table-column prop="created_at" label="添加时间" min-width="190" /><el-table-column label="操作" width="100" fixed="right"><template #default="scope"><el-button link type="danger" @click="remove(scope.row)">删除</el-button></template></el-table-column></el-table></el-card>
</template>
