<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { connectMokantNode, createMokantNode, deleteMokantNode, disconnectMokantNode, getMokantNodes, updateMokantNode, type MokantNode, type MokantNodeInput } from '../../api/admin'

const loading = ref(true)
const saving = ref(false)
const dialogVisible = ref(false)
const editingID = ref<number | null>(null)
const operatingID = ref<number | null>(null)
const nodes = ref<MokantNode[]>([])
const form = ref<MokantNodeInput>({ name: '', host: '127.0.0.1', port: 3001, token: '', is_default: false })

function statusType(node: MokantNode) {
  if (node.status.connected) return 'success'
  if (node.status.last_error) return 'danger'
  return 'info'
}

async function load() {
  loading.value = true
  try {
    const result = await getMokantNodes()
    nodes.value = result.data || []
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '节点列表加载失败')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingID.value = null
  form.value = { name: '', host: '127.0.0.1', port: 3001, token: '', is_default: nodes.value.length === 0 }
  dialogVisible.value = true
}

function openEdit(node: MokantNode) {
  editingID.value = node.id
  form.value = { name: node.name, host: node.host, port: node.port, token: '', is_default: node.is_default }
  dialogVisible.value = true
}

async function save() {
  const data = { ...form.value, name: form.value.name.trim(), host: form.value.host.trim() }
  if (!data.name || !data.host || !Number.isInteger(data.port) || data.port < 1 || data.port > 65535) {
    ElMessage.warning('请完整填写合法的节点名称、地址和端口')
    return
  }
  saving.value = true
  try {
    if (editingID.value === null) await createMokantNode(data)
    else await updateMokantNode(editingID.value, data)
    ElMessage.success(editingID.value === null ? '节点已创建' : '节点已更新')
    dialogVisible.value = false
    await load()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '节点保存失败')
  } finally {
    saving.value = false
  }
}

async function toggleConnection(node: MokantNode) {
  operatingID.value = node.id
  try {
    const result = node.status.connected ? await disconnectMokantNode(node.id) : await connectMokantNode(node.id)
    if (result.data) node.status = result.data
    ElMessage.success(node.status.connected ? '节点连接请求已发送' : '节点已断开')
    await new Promise(resolve => setTimeout(resolve, 350))
    await load()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '节点连接状态更新失败')
  } finally {
    operatingID.value = null
  }
}

async function remove(node: MokantNode) {
  try {
    await ElMessageBox.confirm(`确定删除节点“${node.name}”吗？`, '删除节点', { type: 'warning' })
    await deleteMokantNode(node.id)
    ElMessage.success('节点已删除')
    await load()
  } catch (error) {
    if (error !== 'cancel') ElMessage.error(error instanceof Error ? error.message : '节点删除失败')
  }
}

onMounted(load)
</script>

<template>
  <div class="page-heading">
    <div><h1>听雨节点</h1></div>
    <div><el-button type="primary" @click="openCreate">新增节点</el-button></div>
  </div>
  <el-card v-loading="loading">
    <el-empty v-if="!nodes.length && !loading" description="暂无节点，请先新增节点" />
    <el-table v-else :data="nodes" stripe>
      <el-table-column label="节点名称" min-width="150"><template #default="scope">{{ scope.row.name }} <el-tag v-if="scope.row.is_default" size="small">默认</el-tag></template></el-table-column>
      <el-table-column label="连接地址" min-width="190"><template #default="scope">{{ scope.row.host }}:{{ scope.row.port }}</template></el-table-column>
      <el-table-column label="Token" width="100"><template #default="scope">{{ scope.row.token_set ? '已配置' : '未配置' }}</template></el-table-column>
      <el-table-column label="节点状态" min-width="130"><template #default="scope"><el-tag :type="statusType(scope.row)">{{ scope.row.status.state || (scope.row.status.connected ? '已连接' : '未连接') }}</el-tag></template></el-table-column>
      <el-table-column label="最近检查" min-width="190"><template #default="scope">{{ scope.row.status.last_checked || '-' }}</template></el-table-column>
      <el-table-column label="最近错误" min-width="220"><template #default="scope">{{ scope.row.status.last_error || '-' }}</template></el-table-column>
      <el-table-column label="操作" fixed="right" width="230"><template #default="scope"><el-button link :type="scope.row.status.connected ? 'warning' : 'success'" :loading="operatingID === scope.row.id" @click="toggleConnection(scope.row)">{{ scope.row.status.connected ? '断开' : '连接' }}</el-button><el-button link type="primary" @click="openEdit(scope.row)">编辑</el-button><el-button link type="danger" :disabled="scope.row.is_default" @click="remove(scope.row)">删除</el-button></template></el-table-column>
    </el-table>
  </el-card>
  <el-dialog v-model="dialogVisible" :title="editingID === null ? '新增节点' : '编辑节点'" width="min(520px, calc(100vw - 32px))">
    <el-form label-width="90px" @submit.prevent="save">
      <el-form-item label="节点名称"><el-input v-model="form.name" maxlength="50" placeholder="请输入节点名称" /></el-form-item>
      <el-form-item label="主机地址"><el-input v-model="form.host" placeholder="例如 127.0.0.1" /></el-form-item>
      <el-form-item label="端口"><el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" /></el-form-item>
      <el-form-item label="Token"><el-input v-model="form.token" type="password" show-password autocomplete="new-password" :placeholder="editingID === null ? '请输入服务令牌' : '留空则保留原 Token'" /></el-form-item>
      <el-form-item label="默认节点"><el-switch v-model="form.is_default" /><span class="form-help">新增 QQ 时默认选择此节点</span></el-form-item>
    </el-form>
    <template #footer><el-button @click="dialogVisible = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存</el-button></template>
  </el-dialog>
</template>
