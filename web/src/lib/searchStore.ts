import { create } from 'zustand'

interface SearchState {
  query: string
  setQuery: (q: string) => void
}

// Shared search state so the persistent TopBar input and the Search page are two
// views of the same query — typing in either updates both.
export const useSearch = create<SearchState>((set) => ({
  query: '',
  setQuery: (query) => set({ query }),
}))
