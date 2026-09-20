<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { createAdminQQPlan, deleteAdminQQPlan, getAdminQQPlans, updateAdminQQPlan, type AdminQQPlan } from '../../api/admin'

const loading = ref(false)
const saving = ref(false)
const plans = ref<AdminQQPlan[]>([])
const dialogVisible = ref(false)
const editingID = ref<number | null>(null)
const form = reactive({ name: '', price: 0, service_days: 30, description: '', status: 'active' as 'active' | 'inactive', sort_order: 0 })
async function load() { loading.value = true; try { const result = await getAdminQQPlans(); plans.value = result.data || [] } catch (error) { ElMessage.error(error instanceof Error ? error.message : '套餐加载失败') } finally { loading.value = false } }
function openCreate() { editingID.value = null; Object.assign(form, { name: '', price: 0, service_days: 30, description: '', status: 'active', sort_order: 0 }); dialogVisible.value = true }
function openEdit(plan: AdminQQPlan) { editingID.value = plan.id; Object.assign(form, { name: plan.name, price: plan.price, service_days: plan.service_days, description: plan.description, status: plan.status, sort_order: plan.sort_order }); dialogVisible.value = true }
async function save() { if (!form.name.trim() || !Number.isInteger(form.price) || form.price < 0 || !Number.isInteger(form.service_days) || form.service_days < 1) { ElMessage.warning('请填写有效的套餐名称、金币和服务天数'); return }; saving.value = true; try { const data = { ...form, name: form.name.trim(), description: form.description.trim() }; if (editingID.value) await updateAdminQQPlan(editingID.value, data); else await createAdminQQPlan(data); ElMessage.success(editingID.value ? '套餐已更新' : '套餐已创建'); dialogVisible.value = false; await load() } catch (error) { ElMessage.error(error instanceof Error ? error.message : '套餐保存失败') } finally { saving.value = false } }
async function remove(plan: AdminQQPlan) { if (plan.id === 1) return; await ElMessageBox.confirm(`确定删除套餐“${plan.name}”吗？`, '删除套餐', { type: 'warning' }); try { await deleteAdminQQPlan(plan.id); ElMessage.success('套餐已删除'); await load() } catch (error) { ElMessage.error(error instanceof Error ? error.message : '套餐删除失败') } }
onMounted(load)
</script>
<template>
  <div class="page-heading"><div><h1>QQ 套餐</h1></div><el-button type="primary" @click="openCreate">新增套餐</el-button></div>
  <el-card v-loading="loading"><el-table :data="plans" empty-text="暂无套餐">
    <el-table-column label="名称" prop="name" min-width="150" />
    <el-table-column label="价格" min-width="100"><template #default="scope">{{ scope.row.price }} 金币</template></el-table-column>
    <el-table-column label="服务天数" min-width="110"><template #default="scope">{{ scope.row.service_days }} 天</template></el-table-column>
    <el-table-column label="状态" min-width="90"><template #default="scope"><el-tag :type="scope.row.status === 'active' ? 'success' : 'info'">{{ scope.row.status === 'active' ? '启用' : '停用' }}</el-tag></template></el-table-column>
    <el-table-column label="说明" prop="description" min-width="220" />
    <el-table-column label="操作" width="170"><template #default="scope"><el-button link type="primary" @click="openEdit(scope.row)">编辑</el-button><el-button v-if="scope.row.id !== 1" link type="danger" @click="remove(scope.row)">删除</el-button><span v-if="scope.row.id === 1" class="form-help">不可删除</span></template></el-table-column>
  </el-table></el-card>
  <el-dialog v-model="dialogVisible" :title="editingID ? '编辑套餐' : '新增套餐'" width="min(480px, calc(100vw - 32px))">
    <el-form label-position="top"><el-form-item label="套餐名称"><el-input v-model="form.name" maxlength="30" /></el-form-item><el-form-item label="金币"><el-input-number v-model="form.price" :min="0" :max="2147483647" :step="1" step-strictly controls-position="right" /></el-form-item><el-form-item label="服务天数"><el-input-number v-model="form.service_days" :min="1" :max="36500" :step="1" step-strictly controls-position="right" /></el-form-item><el-form-item label="状态"><el-switch v-model="form.status" active-value="active" inactive-value="inactive" active-text="启用" inactive-text="停用" /></el-form-item><el-form-item label="排序"><el-input-number v-model="form.sort_order" :min="0" :max="2147483647" :step="1" step-strictly controls-position="right" /></el-form-item><el-form-item label="说明"><el-input v-model="form.description" type="textarea" maxlength="100" /></el-form-item></el-form>
    <template #footer><el-button @click="dialogVisible = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存</el-button></template>
  </el-dialog>
</template>
