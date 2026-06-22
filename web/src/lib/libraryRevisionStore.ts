import { create } from 'zustand'

interface LibraryRevisionStore {
  revision: number
  bump: () => void
}

export const useLibraryRevision = create<LibraryRevisionStore>((set) => ({
  revision: 0,
  bump: () => set((s) => ({ revision: s.revision + 1 })),
}))
