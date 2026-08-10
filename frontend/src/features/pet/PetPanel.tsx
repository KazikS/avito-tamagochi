import {
  Badge,
  Button,
  Card,
  Heading,
  Progress,
  SimpleGrid,
  Spinner,
  Stack,
  Text,
} from '@chakra-ui/react';
import type { PetActionKind, PetMood, Stats } from '@/shared/types';
import { useActMutation, useGetPetQuery } from './api';
import { useLiveUpdates } from './useLiveUpdates';
import { CreatePetForm } from './CreatePetForm';
import { toaster } from '@/shared/ui/theme/toaster-instance';
import { newUuid } from '@/shared/lib/uuid';

const STAT_LABELS: Record<keyof Stats, string> = {
  hunger: 'Сытость',
  joy: 'Радость',
  clean: 'Чистота',
  energy: 'Энергия',
};

const MOOD_LABELS: Record<PetMood, string> = {
  radiant: 'Сияет',
  happy: 'Доволен',
  neutral: 'Спокоен',
  sad: 'Грустит',
  sick: 'Болеет',
  sleeping: 'Спит',
};

const MOOD_PALETTE: Record<PetMood, string> = {
  radiant: 'yellow',
  happy: 'green',
  neutral: 'gray',
  sad: 'orange',
  sick: 'red',
  sleeping: 'purple',
};

const ACTION_LABELS: Record<PetActionKind, string> = {
  feed: 'Покормить',
  play: 'Поиграть',
  wash: 'Помыть',
  sleep: 'Уложить спать',
  wake: 'Разбудить',
};

const ACTION_ORDER: PetActionKind[] = ['feed', 'play', 'wash', 'sleep', 'wake'];

export function PetPanel() {
  useLiveUpdates();
  const { data: pet, error, isLoading } = useGetPetQuery();
  const [act, { isLoading: isActing }] = useActMutation();

  if (isLoading) {
    return <Spinner />;
  }

  if (error && 'status' in error && error.status === 404) {
    return <CreatePetForm />;
  }

  if (error || !pet) {
    return <Text color="fg.error">Не удалось загрузить питомца. Обновите страницу.</Text>;
  }

  const handleAct = (kind: PetActionKind) => {
    act({ actionId: newUuid(), kind })
      .unwrap()
      .catch((err: unknown) => {
        const message =
          err && typeof err === 'object' && 'message' in err
            ? String(err.message)
            : 'Не удалось выполнить действие';
        toaster.create({ type: 'error', title: message });
      });
  };

  return (
    <Card.Root maxW="lg" mx="auto">
      <Card.Header>
        <Stack direction="row" justify="space-between" align="center">
          <Heading size="md">{pet.name}</Heading>
          <Badge colorPalette={MOOD_PALETTE[pet.mood]}>{MOOD_LABELS[pet.mood]}</Badge>
        </Stack>
        <Text color="fg.muted">
          Уровень {pet.level} · {pet.stageLabel ?? `стадия ${pet.stage}`}
        </Text>
      </Card.Header>
      <Card.Body>
        <Stack gap="5">
          {/* xpToNext — сколько опыта ОСТАЛОСЬ до следующего уровня
              (config.Progress.ToNext), не общая стоимость уровня: ширина
              шкалы — xp (уже набрано) + xpToNext (осталось), 0 на максимуме
              уровня. */}
          <Progress.Root
            value={pet.xp}
            max={Math.max(pet.xp + pet.xpToNext, 1)}
            colorPalette="blue"
          >
            <Progress.Label>
              {pet.xpToNext > 0
                ? `Ещё ${pet.xpToNext} XP до уровня ${pet.level + 1}`
                : 'Максимальный уровень'}
            </Progress.Label>
            <Progress.Track>
              <Progress.Range />
            </Progress.Track>
          </Progress.Root>

          <SimpleGrid columns={2} gap="4">
            {(Object.keys(STAT_LABELS) as (keyof Stats)[]).map((key) => (
              <Progress.Root key={key} value={pet.stats[key]} max={100} colorPalette="green">
                <Progress.Label>
                  {STAT_LABELS[key]} {pet.stats[key]}
                </Progress.Label>
                <Progress.Track>
                  <Progress.Range />
                </Progress.Track>
              </Progress.Root>
            ))}
          </SimpleGrid>

          <SimpleGrid columns={{ base: 2, sm: 3 }} gap="2">
            {ACTION_ORDER.map((kind) => {
              const state = pet.actions[kind];
              const disabled =
                isActing || (state ? state.remaining <= 0 && kind !== 'wake' : false);
              return (
                <Button
                  key={kind}
                  onClick={() => {
                    handleAct(kind);
                  }}
                  disabled={disabled}
                  variant="subtle"
                  colorPalette="blue"
                >
                  {ACTION_LABELS[kind]}
                  {state ? ` (${state.remaining})` : ''}
                </Button>
              );
            })}
          </SimpleGrid>
        </Stack>
      </Card.Body>
    </Card.Root>
  );
}
