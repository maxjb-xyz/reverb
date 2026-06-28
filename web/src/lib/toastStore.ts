import { create } from 'zustand'

export type ToastKind = 'success' | 'error' | 'info'

export interface Toast {
  id: string
  message: string
  kind: ToastKind
}

interface ToastStore {
  toasts: Toast[]
  push(message: string, kind: ToastKind): void
  dismiss(id: string): void
}

export const useToastStore = create<ToastStore>((set) => ({
  toasts: [],

  push: (message, kind) => {
    const id = `toast-${Date.now()}-${Math.random().toString(36).slice(2)}`
    set((s) => ({ toasts: [...s.toasts, { id, message, kind }] }))
    setTimeout(() => {
      set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }))
    }, 5000)
  },

  dismiss: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}))
