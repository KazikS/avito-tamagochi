import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './app/App';
import { Provider as ChakraProvider } from '@/shared/ui/theme';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ChakraProvider>
      <App />
    </ChakraProvider>
  </StrictMode>,
);
