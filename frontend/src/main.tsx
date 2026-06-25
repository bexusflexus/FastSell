import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import '@vitejs/plugin-react/preamble';
import { App } from './App';
import { AppErrorBoundary } from './components/AppErrorBoundary';
import './styles.css';

const rootElement = document.getElementById('root');

if (!rootElement) {
  throw new Error('FastSell frontend could not find the root element.');
}

createRoot(rootElement).render(
  <StrictMode>
    <AppErrorBoundary>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </AppErrorBoundary>
  </StrictMode>,
);
