import { baseApi } from '@/shared/api/baseApi';
import type { LeaderboardPage } from '@/shared/types';

export const leaderboardApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // scope=top — единственный реализованный на бэке (internal/social,
    // docs/RECONCILIATION.md): league/friends отвечают понятной 422, фронт
    // их пока не запрашивает, а не подставляет вместо них top молча.
    getLeaderboard: builder.query<LeaderboardPage, void>({
      query: () => ({ url: '/leaderboard', params: { scope: 'top', limit: 10 } }),
      providesTags: ['Leaderboard'],
    }),
  }),
});

export const { useGetLeaderboardQuery } = leaderboardApi;
