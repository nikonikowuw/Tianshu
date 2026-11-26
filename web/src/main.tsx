import { StrictMode } from 'react'
import ReactDOM from 'react-dom/client'
import '@/i18n/index'
import { AppProviders } from '@/app/providers'
import './styles/index.css'

const rootElement = document.getElementById('root')!
if (!rootElement.innerHTML) {
  ReactDOM.createRoot(rootElement).render(
    <StrictMode>
      <AppProviders />
    </StrictMode>
  )
}
