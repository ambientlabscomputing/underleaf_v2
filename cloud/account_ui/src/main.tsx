import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { QueryClientProvider } from '@tanstack/react-query';
import { ReactQueryDevtools } from '@tanstack/react-query-devtools';
import { AppThemeProvider } from './components';
import { AuthProvider } from './auth/AuthProvider';
import { queryClient } from './datastore';
import { App } from './App';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <AppThemeProvider>
        <QueryClientProvider client={queryClient}>
          <AuthProvider>
            <App />
          </AuthProvider>
          <ReactQueryDevtools />
        </QueryClientProvider>
      </AppThemeProvider>
    </BrowserRouter>
  </StrictMode>,
);
