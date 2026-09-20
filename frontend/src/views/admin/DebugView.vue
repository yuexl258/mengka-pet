<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'

interface ParamRow { key: string; value: string; type: 'string' | 'number' }
const action = ref('get_bot_list')
const params = ref<ParamRow[]>([
  { key: 'self_id', value: '', type: 'number' },
])
const response = ref('')
const loading = ref(false)
const connected = ref(false)

function addParam() { params.value.push({ key: '', value: '', type: 'string' }) }
function removeParam(index: number) { params.value.splice(index, 1) }
function parseValue(item: ParamRow): string | number {
  if (item.type === 'string') return item.value
  const value = Number(item.value.trim())
  if (!Number.isFinite(value)) throw new Error(`参数“${item.key}”必须是数字`)
  return value
}
async function send() {
  const name = action.value.trim()
  if (!name) { ElMessage.warning('请输入 API') ; return }
  const data: Record<string, unknown> = {}
  for (const item of params.value) {
    const key = item.key.trim()
    if (!key) { ElMessage.warning('请完整填写参数名称'); return }
    try {
      data[key] = parseValue(item)
    } catch (error) {
      ElMessage.warning(error instanceof Error ? error.message : '参数类型不正确')
      return
    }
  }
  loading.value = true
  response.value = ''
  try {
    const result = await fetch('/api/admin/mokant/debug', { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action: name, params: data }) })
    const raw = await result.text()
    response.value = raw
    connected.value = result.ok
    if (!result.ok) ElMessage.error('请求失败，请查看原始返回数据')
  } catch (error) {
    connected.value = false
    response.value = JSON.stringify({ error: error instanceof Error ? error.message : '请求失败' }, null, 2)
    ElMessage.error('调试请求失败')
  } finally { loading.value = false }
}
function clearResponse() { response.value = '' }
</script>

<template>
  <div class="page-heading"><div><h1>WS 调试</h1><p>通过已连接的听雨框架 WebSocket 执行自定义 API，并保留原始返回数据。</p></div></div>
  <el-row :gutter="16">
    <el-col :xs="24" :lg="10">
      <el-card class="debug-card">
        <template #header><div class="debug-card-title"><strong>请求参数</strong><el-tag size="small" :type="connected ? 'success' : 'info'">{{ connected ? '请求成功' : '等待请求' }}</el-tag></div></template>
        <el-form label-position="top" @submit.prevent="send">
          <el-form-item label="API"><el-input v-model="action" placeholder="例如 get_pet_profile" /></el-form-item>
          <div class="debug-params-head"><span>自定义数据（按顺序填写）</span><el-button link type="primary" @click="addParam">新增参数</el-button></div>
          <div v-if="!params.length" class="debug-empty">暂无参数，点击“新增参数”添加</div>
          <div v-for="(item, index) in params" :key="index" class="debug-param-row">
            <span class="debug-param-index">{{ index + 1 }}</span>
            <el-input v-model="item.key" placeholder="参数名" />
            <el-select v-model="item.type" class="debug-param-type">
              <el-option label="string" value="string" />
              <el-option label="number" value="number" />
            </el-select>
            <el-input v-model="item.value" :type="item.type === 'number' ? 'number' : 'text'" placeholder="参数值" />
            <el-button text type="danger" @click="removeParam(index)">删除</el-button>
          </div>
          <div class="debug-actions"><el-button type="primary" :loading="loading" @click="send">发送请求</el-button><el-button @click="clearResponse">清空返回</el-button></div>
        </el-form>
      </el-card>
    </el-col>
    <el-col :xs="24" :lg="14">
      <el-card class="debug-card debug-result-card">
        <template #header><div class="debug-card-title"><strong>原始返回数据</strong><span>不做业务解析</span></div></template>
        <pre v-loading="loading" class="debug-result">{{ response || '发送请求后显示 WebSocket 原始响应' }}</pre>
      </el-card>
    </el-col>
  </el-row>
  <MokantConsole />
</template>
