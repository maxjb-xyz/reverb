import { create } from 'zustand'

export type SearchMode = 'library' | 'everywhere'

interface SearchState {
  query: string
  mode: SearchMode
  setQuery: (q: string) => void
  setMode: (m: SearchMode) => void
}

// Shared search state so the persistent TopBar input and the Search page are two
// views of the same query — typing in either updates both.
export const useSearch = create<SearchState>((set) => ({
  query: '',
  mode: 'library',
  setQuery: (query) => set({ query }),
  setMode: (mode) => set({ mode }),
}))
