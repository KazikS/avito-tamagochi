import { baseApi } from '@/shared/api/baseApi';
import type { DailySummary } from '@/shared/types';

export const summaryApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // Без date — «сегодня» по часам сервера (docs/openapi.json: «решение о
    // показе принимает СЕРВЕР» — фронт не считает сутки сам).
    getDailySummary: builder.query<DailySummary, void>({
      query: () => '/summary/daily',
      providesTags: ['DailySummary'],
    }),
    markDailySummarySeen: builder.mutation<void, { date: string }>({
      query: (body) => ({ url: '/summary/daily/seen', method: 'POST', body }),
    }),
  }),
});

export const { useGetDailySummaryQuery, useMarkDailySummarySeenMutation } = summaryApi;
