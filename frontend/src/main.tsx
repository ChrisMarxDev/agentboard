import { createRoot } from 'react-dom/client'
import App from './App'
import './index.css'

// Apply stored theme before React mounts to avoid a flash of the wrong theme.
const stored = localStorage.getItem('agentboard:theme')
if (stored === 'light' || stored === 'dark') {
  document.documentElement.setAttribute('data-theme', stored)
}

createRoot(document.getElementById('root')!).render(<App />)
