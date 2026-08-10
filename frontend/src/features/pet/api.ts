import { baseApi } from '@/shared/api/baseApi';
import type { ActionResult, Pet, PetActionKind, PresetId } from '@/shared/types';

// injectEndpoints поверх baseApi, не createApi: единственный инстанс
// объявлен в shared/api/baseApi.ts (eslint no-restricted-imports это уже
// проверяет), здесь только добавляются конечные точки тега pet.

export interface CreatePetRequest {
  presetId: PresetId;
  name?: string;
}

export interface ActRequest {
  /** UUID с клиента — защита от двойного тапа, генерируется в компоненте на каждый клик. */
  actionId: string;
  kind: PetActionKind;
}

export const petApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    getPet: builder.query<Pet, void>({
      query: () => '/pet',
      providesTags: ['Pet'],
    }),
    createPet: builder.mutation<Pet, CreatePetRequest>({
      query: (body) => ({ url: '/pets', method: 'POST', body }),
      invalidatesTags: ['Pet'],
    }),
    act: builder.mutation<ActionResult, ActRequest>({
      query: (body) => ({ url: '/pet/actions', method: 'POST', body }),
      // Инвалидация, а не ручной патч кэша: POST /pet/actions уже возвращает
      // ПОЛНОЕ состояние (docs/openapi.json — «чтобы фронт не собирал его по
      // кускам»), но через тег проще держать один путь обновления кэша,
      // общий с WS-пушем (useLiveUpdates шлёт тот же invalidateTags).
      // Rewards — тоже: действие начисляет XP, а значит могло открыть новую
      // награду (eligibility в internal/rewards считается по уровню питомца);
      // без этого панель наград показывала бы «заблокировано» до ручного
      // обновления страницы — найдено вручную, кликами в реальном браузере,
      // не только чтением кода.
      invalidatesTags: ['Pet', 'Rewards'],
    }),
  }),
});

export const { useGetPetQuery, useCreatePetMutation, useActMutation } = petApi;
