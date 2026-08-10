import { configureStore } from '@reduxjs/toolkit';
import { useDispatch } from 'react-redux';
import { baseApi } from '@/shared/api/baseApi';
import { debugApi } from '@/shared/api/debugApi';

export const store = configureStore({
  reducer: {
    [baseApi.reducerPath]: baseApi.reducer,
    [debugApi.reducerPath]: debugApi.reducer,
  },
  middleware: (getDefaultMiddleware) =>
    getDefaultMiddleware().concat(baseApi.middleware, debugApi.middleware),
});

export type RootState = ReturnType<typeof store.getState>;
export type AppDispatch = typeof store.dispatch;
export const useAppDispatch = useDispatch.withTypes<AppDispatch>();
