<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { Delete, Minus, Plus } from '@element-plus/icons-vue'

type LogEvent = { type: string; timestamp: string; level: string; message: string; event: string; raw?: string }
const collapsed = ref(false)
const logs = ref<LogEvent[]>([])
const connected = ref(false)
const socket = ref<WebSocket | null>(null)

function connect() {
  if (socket.value && socket.value.readyState <= WebSocket.OPEN) return
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const current = new WebSocket(`${protocol}//${window.location.host}/ws/admin/mokant/logs`)
  socket.value = current
  current.onopen = () => { connected.value = true }
  current.onmessage = event => {
    try {
      const item = JSON.parse(event.data) as LogEvent
      logs.value.push(item)
      if (logs.value.length > 200) logs.value.splice(0, logs.value.length - 200)
    } catch {}
  }
  current.onclose = () => {
    connected.value = false
    socket.value = null
  }
  current.onerror = () => { connected.value = false }
}

function clearLogs() { logs.value = [] }
function formatTime(value: string) { return value ? new Date(value).toLocaleTimeString('zh-CN', { hour12: false, timeZone: 'Asia/Shanghai' }) : '' }
onMounted(connect)
onBeforeUnmount(() => { socket.value?.close(); socket.value = null })
</script>

<template>
  <section class="mokant-console" :class="{ 'is-collapsed': collapsed }">
    <header class="mokant-console-head"><span>听雨事件控制台</span><span class="mokant-console-state" :class="{ connected }">{{ connected ? '已连接' : '已断开' }}</span><div class="mokant-console-actions"><el-button text :icon="Delete" aria-label="清空日志" @click="clearLogs" /><el-button text :icon="collapsed ? Plus : Minus" :aria-label="collapsed ? '展开萌卡控制台' : '收起萌卡控制台'" @click="collapsed = !collapsed" /></div></header>
    <div v-if="!collapsed" class="mokant-console-body"><div v-if="!logs.length" class="mokant-console-empty">等待 QQ 离线事件</div><div v-for="(item, index) in logs" :key="`${item.timestamp}-${index}`" class="mokant-log-line" :class="`level-${item.level}`"><time>{{ formatTime(item.timestamp) }}</time><pre>{{ item.raw || item.message }}</pre></div></div>
  </section>
</template>
