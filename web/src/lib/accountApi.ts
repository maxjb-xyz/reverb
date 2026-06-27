import { api } from './api'

/** POST /account/password — change the current user's password.
 *  Throws ApiError(400) if current password is wrong. */
export async function changePassword(current: string, next: string): Promise<void> {
  await api.post('/account/password', { current, new: next })
}

/** POST /account/logout-all — invalidate all other active sessions for this user. */
export async function logoutAll(): Promise<void> {
  await api.post('/account/logout-all')
}
