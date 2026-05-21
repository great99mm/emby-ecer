import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { Toaster } from 'react-hot-toast'
import App from './App'
import './index.css'

ReactDOM.createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <BrowserRouter>
      <App />
      <Toaster position="top-center" toastOptions={{
        style: { borderRadius: '12px', fontWeight: 600, fontSize: '14px' },
        success: { style: { background: '#ecfdf5', color: '#065f46', border: '1px solid #a7f3d0' } },
        error: { style: { background: '#fef2f2', color: '#991b1b', border: '1px solid #fecaca' } },
      }} />
    </BrowserRouter>
  </React.StrictMode>,
)
