import { create } from 'zustand'

interface LibraryRevisionStore {
  revision: number
  bump: () => void
}

let debounceTimer: ReturnType<typeof setTimeout> | null = null

export const useLibraryRevision = create<LibraryRevisionStore>((set) => ({
  revision: 0,
  bump: () => {
    if (debounceTimer !== null) clearTimeout(debounceTimer)
    debounceTimer = setTimeout(() => {
      debounceTimer = null
      set((s) => ({ revision: s.revision + 1 }))
    }, 300)
  },
}))
