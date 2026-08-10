import { createApi, fetchBaseQuery } from '@reduxjs/toolkit/query/react';
import type { BaseQueryFn } from '@reduxjs/toolkit/query/react';
import type { components } from '@/shared/types/api';

// Единственное место в приложении, где вызывается createApi (правило
// no-restricted-imports из eslint.config.js) — остальные срезы делают
// injectEndpoints поверх этого инстанса.
//
// Бэк всегда отвечает конвертом { data, meta } (docs/openapi.json →
// Envelope). baseQuery разворачивает его один раз здесь, чтобы каждый
// эндпоинт возвращал уже «голые» данные, а не унаследованные data/meta
// в каждом query.

export interface ApiError {
  /** HTTP-статус ответа, 0 — если запрос не дошёл до сети. */
  status: number;
  /** Машинный код ошибки — по нему и должен ветвиться код (см. ErrorCode в контракте). */
  code?: components['schemas']['ErrorCode'];
  /** Текст для пользователя, meta.message с бэка. */
  message: string;
}

interface Envelope<T> {
  data?: T;
  meta?: components['schemas']['Meta'];
}

const rawBaseQuery = fetchBaseQuery({ baseUrl: '/api/v1' });

const envelopeBaseQuery: BaseQueryFn<
  Parameters<typeof rawBaseQuery>[0],
  unknown,
  ApiError
> = async (args, api, extraOptions) => {
  const result = await rawBaseQuery(args, api, extraOptions);

  if (result.error) {
    const body = result.error.data as Envelope<unknown> | undefined;
    return {
      error: {
        status: typeof result.error.status === 'number' ? result.error.status : 0,
        code: body?.meta?.error,
        message: body?.meta?.message ?? 'Не удалось выполнить запрос',
      },
    };
  }

  // 204 No Content и подобные ответы без тела: fetchBaseQuery отдаёт null.
  if (result.data === null || result.data === undefined) {
    return { data: undefined };
  }

  const envelope = result.data as Envelope<unknown>;
  return { data: envelope.data, meta: result.meta };
};

export const baseApi = createApi({
  reducerPath: 'api',
  baseQuery: envelopeBaseQuery,
  tagTypes: ['Pet', 'Leaderboard', 'DailySummary', 'Rewards'],
  endpoints: () => ({}),
});
