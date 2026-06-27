import { useQuery } from '@tanstack/react-query'
import { api } from './api'

export interface User {
  id: string
  username: string
  roleId: string
  roleName: string
  isOwner: boolean
  disabled: boolean
  createdAt: string
  lastSeen: string | null
}

export interface Role {
  id: string
  name: string
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

export function useUsers() {
  return useQuery({ queryKey: ['users', 'list'], queryFn: listUsers })
}

export function useRoles() {
  return useQuery({ queryKey: ['roles', 'list'], queryFn: listRoles })
}
