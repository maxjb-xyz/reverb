import { useQuery } from '@tanstack/react-query'
import { api } from './api'

export interface User {
  id: string
  username: string
  roleId: string
  roleName: string
  isOwner: boolean
  disabled: boolean
  createdAt: number
  lastSeen: number | null
}

export interface Role {
  id: string
  name: string
  isSystem?: boolean
  capabilities?: string[]
}

export interface Capability {
  key: string
  label: string
  description: string
}

export interface CreateRoleReq {
  name: string
  capabilities: string[]
}

export interface UpdateRoleReq {
  name: string
  capabilities: string[]
}

export interface RegistrationPolicy {
  signupEnabled: boolean
  invitesEnabled: boolean
  defaultRoleId: string
}

export interface Invite {
  id: string
  code: string
  roleId: string | null
  roleName: string | null
  expiresAt: number | null
  usedAt: number | null
  createdAt: number
}

export interface CreateInviteReq {
  roleId?: string
  expiresAt?: number
}

export interface CreateUserReq {
  username: string
  password: string
  roleId: string
}

export interface UpdateUserReq {
  roleId?: string
  disabled?: boolean
}

export function listUsers(): Promise<User[]> {
  return api.get<User[]>('/users')
}

export function listRoles(): Promise<Role[]> {
  return api.get<Role[]>('/roles')
}

export function createUser(body: CreateUserReq): Promise<User> {
  return api.post<User>('/users', body)
}

export function updateUser(id: string, body: UpdateUserReq): Promise<User> {
  return api.patch<User>(`/users/${encodeURIComponent(id)}`, body)
}

export function deleteUser(id: string): Promise<void> {
  return api.del<void>(`/users/${encodeURIComponent(id)}`)
}

export function resetPassword(id: string, password: string): Promise<void> {
  return api.post<void>(`/users/${encodeURIComponent(id)}/password`, { password })
}

// ── Roles CRUD ────────────────────────────────────────────────────────────────

export function createRole(body: CreateRoleReq): Promise<Role> {
  return api.post<Role>('/roles', body)
}

export function updateRole(id: string, body: UpdateRoleReq): Promise<Role> {
  return api.patch<Role>(`/roles/${encodeURIComponent(id)}`, body)
}

export function deleteRole(id: string): Promise<void> {
  return api.del<void>(`/roles/${encodeURIComponent(id)}`)
}

// ── Capabilities ──────────────────────────────────────────────────────────────

export function getCapabilities(): Promise<Capability[]> {
  return api.get<Capability[]>('/capabilities')
}

// ── Registration policy ───────────────────────────────────────────────────────

export function getRegistration(): Promise<RegistrationPolicy> {
  return api.get<RegistrationPolicy>('/settings/registration')
}

export function setRegistration(body: RegistrationPolicy): Promise<RegistrationPolicy> {
  return api.patch<RegistrationPolicy>('/settings/registration', body)
}

// ── Invites ───────────────────────────────────────────────────────────────────

export function listInvites(): Promise<Invite[]> {
  return api.get<Invite[]>('/invites')
}

export function createInvite(body?: CreateInviteReq): Promise<{ id: string; code: string }> {
  return api.post<{ id: string; code: string }>('/invites', body ?? {})
}

export function deleteInvite(id: string): Promise<void> {
  return api.del<void>(`/invites/${encodeURIComponent(id)}`)
}

// ── Hooks ─────────────────────────────────────────────────────────────────────

export function useUsers() {
  return useQuery({ queryKey: ['users', 'list'], queryFn: listUsers })
}

export function useRoles() {
  return useQuery({ queryKey: ['roles', 'list'], queryFn: listRoles })
}

export function useCapabilities() {
  return useQuery({ queryKey: ['capabilities', 'list'], queryFn: getCapabilities })
}

export function useRegistration() {
  return useQuery({ queryKey: ['registration', 'policy'], queryFn: getRegistration })
}

export function useInvites() {
  return useQuery({ queryKey: ['invites', 'list'], queryFn: listInvites })
}
