import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { Provider as ReduxProvider } from 'react-redux';
import App from './app/App';
import { store } from './app/store';
import { Provider as ChakraProvider } from '@/shared/ui/theme';
import { Toaster } from '@/shared/ui/theme/toaster';
import '@fontsource-variable/manrope';
import '@fontsource-variable/jetbrains-mono';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ReduxProvider store={store}>
      <ChakraProvider>
        <Toaster />
        <App />
      </ChakraProvider>
    </ReduxProvider>
  </StrictMode>,
);
