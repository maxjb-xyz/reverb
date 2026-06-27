import { describe, it, expect } from 'vitest'
import { ApiError, loginErrorMessage } from './api'

describe('loginErrorMessage', () => {
  it('maps a 401 to an incorrect-password message', () => {
    expect(loginErrorMessage(new ApiError('POST', '/auth/login', 401))).toBe('Incorrect username or password')
  })
  it('maps a 500 to a server-unreachable message', () => {
    expect(loginErrorMessage(new ApiError('POST', '/auth/login', 500))).toMatch(/server/i)
  })
  it('maps a thrown network error to a server-unreachable message', () => {
    expect(loginErrorMessage(new TypeError('Failed to fetch'))).toMatch(/server/i)
  })
})
