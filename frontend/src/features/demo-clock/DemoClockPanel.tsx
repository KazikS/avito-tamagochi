import { Button, Card, HStack, Text } from '@chakra-ui/react';
import { useGetClockQuery, useAdvanceClockMutation } from '@/shared/api/debugApi';
import { petApi } from '@/features/pet/api';
import { useAppDispatch } from '@/app/store';

// Пульт демо-стенда (docs/DECISIONS.md — «не решение, а несделанная
// работа»): жюри смотрит показ несколько минут и не видит распад
// показателей или смену суток без него. Смонтирован на бэке только под
// APP_ENV=demo (backend/cmd/demo_clock.go) — вне демо-режима GET /debug/clock
// отвечает 404, и панель тихо не рендерится, а не падает ошибкой на экране.
export function DemoClockPanel() {
  const { data, error } = useGetClockQuery();
  const [advanceClock, { isLoading }] = useAdvanceClockMutation();
  const dispatch = useAppDispatch();

  if (error || !data) return null;

  const handleAdvance = (advanceHours: number) => {
    advanceClock({ advanceHours })
      .unwrap()
      .then(() => {
        // Сдвиг часов не бьёт по /pet/actions — decay пересчитывается лениво
        // при следующем чтении (internal/pet), поэтому карточку нужно
        // перечитать самим, WS-пуш тут не сработает.
        dispatch(petApi.util.invalidateTags(['Pet', 'DailySummary', 'Leaderboard', 'Rewards']));
      })
      .catch(() => undefined);
  };

  return (
    <Card.Root maxW="lg" mx="auto" variant="subtle">
      <Card.Body>
        <HStack justify="space-between">
          <Text color="fg.muted" fontSize="sm">
            Демо-время: {new Date(data.now).toLocaleString('ru-RU')}
          </Text>
          <HStack gap="2">
            <Button size="xs" onClick={() => handleAdvance(1)} loading={isLoading}>
              +1 час
            </Button>
            <Button size="xs" onClick={() => handleAdvance(24)} loading={isLoading}>
              +24 часа
            </Button>
          </HStack>
        </HStack>
      </Card.Body>
    </Card.Root>
  );
}
