import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import App from './App'
import { getSettings, applyAccent } from './lib/settingsApi'
import './index.css'

// Best-effort: theme the app with the saved accent before the user notices.
// Fails silently when logged out (settings is auth-gated) — the CSS default red wins.
void getSettings()
  .then((s) => applyAccent(s.accentColor))
  .catch(() => {})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <Routes>
        <Route path="/*" element={<App />} />
      </Routes>
    </BrowserRouter>
  </React.StrictMode>,
)
