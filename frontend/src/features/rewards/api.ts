import { baseApi } from '@/shared/api/baseApi';
import type { Reward } from '@/shared/types';
import { newUuid } from '@/shared/lib/uuid';

export const rewardsApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    getRewards: builder.query<Reward[], void>({
      query: () => '/rewards',
      providesTags: ['Rewards'],
    }),
    // Idempotency-Key генерируется на каждый вызов мутации — тот же приём,
    // что actionId в features/pet/api.ts: сервер отличает легитимный ретрай
    // (тот же ключ) от второй попытки получить уже выданную награду (другой
    // ключ, 409 REWARD_ALREADY_CLAIMED с той же наградой в data).
    claimReward: builder.mutation<Reward, { rewardId: string }>({
      query: ({ rewardId }) => ({
        url: `/rewards/${rewardId}/claim`,
        method: 'POST',
        headers: { 'Idempotency-Key': newUuid() },
      }),
      // Инвалидация и на ошибке тоже: 409 REWARD_ALREADY_CLAIMED значит, что
      // реальное состояние на сервере уже другое (например, получено из
      // другой вкладки) — список обязан подтянуться к реальности, а не
      // остаться показывать «доступно».
      invalidatesTags: ['Rewards'],
    }),
    redeemReward: builder.mutation<Reward, { rewardId: string }>({
      query: ({ rewardId }) => ({ url: `/rewards/${rewardId}/redeem-click`, method: 'POST' }),
      invalidatesTags: ['Rewards'],
    }),
  }),
});

export const { useGetRewardsQuery, useClaimRewardMutation, useRedeemRewardMutation } = rewardsApi;
