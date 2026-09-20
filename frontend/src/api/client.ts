export interface ApiEnvelope<T> {
  code: number
  message: string
  data: T | null
}

export type ApiError = Error & { code?: number; status?: number; requestId?: string }

export async function get<T>(path: string): Promise<ApiEnvelope<T>> {
  return request<T>(path, { method: 'GET' })
}

export async function post<T>(path: string, data?: unknown): Promise<ApiEnvelope<T>> {
  const body = data === undefined ? undefined : typeof data === 'string' ? data : JSON.stringify(data)
  return request<T>(path, { method: 'POST', body })
}

export async function downloadStrangerTXT(page: number, pageSize: number, all: boolean): Promise<void> {
  const response = await fetch(`/api/admin/stranger-pets/export?page=${page}&page_size=${pageSize}${all ? '&all=true' : ''}`, { credentials: 'include' })
  if (!response.ok) throw new Error('导出失败')
  const blob = await response.blob()
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = 'pet-pk-strangers.txt'
  link.click()
  URL.revokeObjectURL(url)
}

export async function patch<T>(path: string, data: unknown): Promise<ApiEnvelope<T>> {
  return request<T>(path, { method: 'PATCH', body: JSON.stringify(data) })
}

export async function put<T>(path: string, data: unknown): Promise<ApiEnvelope<T>> {
  return request<T>(path, { method: 'PUT', body: JSON.stringify(data) })
}

export async function del<T>(path: string): Promise<ApiEnvelope<T>> {
  return request<T>(path, { method: 'DELETE' })
}

async function request<T>(path: string, options: RequestInit): Promise<ApiEnvelope<T>> {
  let response: Response
  const requestId = createRequestId()
  const controller = options.signal ? null : new AbortController()
  const timeout = controller ? window.setTimeout(() => controller.abort(), 10000) : null
  try {
    response = await fetch(path, {
      ...options,
      credentials: 'include',
      signal: options.signal || controller?.signal,
      headers: { 'Content-Type': 'application/json', 'X-Request-ID': requestId, ...(options.headers || {}) },
    })
  } catch {
    const error = new Error('无法连接服务器，请确认 Go 后端已启动') as ApiError
    error.requestId = requestId
    throw error
  } finally {
    if (timeout !== null) window.clearTimeout(timeout)
  }

  const raw = await response.text()
  let body: ApiEnvelope<T> | null = null
  if (raw.trim()) {
    try {
      body = JSON.parse(raw) as ApiEnvelope<T>
    } catch {
      const error = new Error(response.status >= 500 ? '服务器暂时不可用，请稍后再试' : `请求失败（HTTP ${response.status}）`) as ApiError
      error.status = response.status; error.requestId = response.headers.get('X-Request-ID') || requestId
      throw error
    }
  }

  if (!body) {
    const error = new Error(response.status >= 500 ? '服务器暂时不可用，请稍后再试' : `请求失败（HTTP ${response.status}）`) as ApiError
    error.status = response.status; error.requestId = response.headers.get('X-Request-ID') || requestId
    throw error
  }

  if (!response.ok || body.code !== 0) {
    const error = new Error(body.message || '请求失败') as ApiError
    error.code = body.code
    error.status = response.status
		error.requestId = response.headers.get('X-Request-ID') || requestId
    throw error
  }
  return body
}

export function createRequestId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  if (typeof crypto !== 'undefined' && typeof crypto.getRandomValues === 'function') {
    const bytes = new Uint8Array(16)
    crypto.getRandomValues(bytes)
    bytes[6] = (bytes[6] & 0x0f) | 0x40
    bytes[8] = (bytes[8] & 0x3f) | 0x80
    return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('').replace(
      /^(.{8})(.{4})(.{4})(.{4})(.{12})$/,
      '$1-$2-$3-$4-$5'
    )
  }
  return `request-${Date.now()}-${Math.random().toString(16).slice(2)}`
}
