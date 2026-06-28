import { useQuery } from '@tanstack/react-query'
import { api } from './api'

export interface ConfigField {
  key: string
  label: string
  type: string
  required: boolean
  secret: boolean
}
export interface ConfigSchema {
  fields: ConfigField[]
}
export interface AvailableAdapter {
  type: string
  name: string
  configSchema: ConfigSchema
  capabilities: string[]
}
export interface AdapterInstance {
  id: string
  type: string
  name: string
  enabled: boolean
  priority: number
  config: Record<string, unknown>
  capabilities: string[]
}
export interface TestResult {
  ok: boolean
  error?: string
}
export interface CreateAdapterReq {
  type: string
  name: string
  enabled: boolean
  priority: number
  config: Record<string, unknown>
}
export interface UpdateAdapterReq {
  name: string
  enabled: boolean
  priority: number
  config: Record<string, unknown>
}
interface Wrapped<T> {
  data: T
  pendingRestart: boolean
}

export const SECRET_SENTINEL = '••••••••'

export function listAvailable(): Promise<AvailableAdapter[]> {
  return api.get<AvailableAdapter[]>('/adapters/available')
}
export function listAdapters(): Promise<AdapterInstance[]> {
  return api.get<AdapterInstance[]>('/adapters')
}
export function createAdapter(b: CreateAdapterReq): Promise<Wrapped<AdapterInstance>> {
  return api.post<Wrapped<AdapterInstance>>('/adapters', b)
}
export function updateAdapter(id: string, b: UpdateAdapterReq): Promise<Wrapped<AdapterInstance>> {
  return api.put<Wrapped<AdapterInstance>>(`/adapters/${encodeURIComponent(id)}`, b)
}
export function deleteAdapter(id: string): Promise<{ ok: boolean; pendingRestart: boolean }> {
  return api.del<{ ok: boolean; pendingRestart: boolean }>(`/adapters/${encodeURIComponent(id)}`)
}
export function testAdapter(name: string, config: Record<string, unknown>): Promise<TestResult> {
  return api.post<TestResult>('/adapters/test', { name, config })
}

export function useAvailableAdapters() {
  return useQuery({ queryKey: ['adapters', 'available'], queryFn: listAvailable })
}
export function useAdapters() {
  return useQuery({ queryKey: ['adapters', 'list'], queryFn: listAdapters })
}
