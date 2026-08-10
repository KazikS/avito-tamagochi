import { createApi, fetchBaseQuery } from '@reduxjs/toolkit/query/react';

// Пульт демо-стенда (backend/cmd/demo_clock.go): вне docs/openapi.json и вне
// конверта { data, meta }, поэтому отдельный, а не injectEndpoints поверх
// baseApi — тому положено разворачивать конверт, а тут его нет. Доступен
// только при APP_ENV=demo на бэке; вне демо-режима роут просто не смонтирован.
export const debugApi = createApi({
  reducerPath: 'debugApi',
  baseQuery: fetchBaseQuery({ baseUrl: '/debug' }),
  endpoints: (builder) => ({
    getClock: builder.query<{ now: string }, void>({
      query: () => '/clock',
    }),
    advanceClock: builder.mutation<{ now: string }, { advanceHours: number }>({
      query: (body) => ({ url: '/clock/advance', method: 'POST', body }),
    }),
  }),
});

export const { useGetClockQuery, useAdvanceClockMutation } = debugApi;
