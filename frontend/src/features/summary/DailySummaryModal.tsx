import { useState } from 'react';
import { Button, Dialog, Portal, Stack, Text } from '@chakra-ui/react';
import { useGetDailySummaryQuery, useMarkDailySummarySeenMutation } from './api';

// shouldShow решает сервер (docs/openapi.json), но «уже закрыл в этой
// сессии» — чисто фронтовое состояние: без него ре-рендер (например, после
// WS-обновления питомца) снова открыл бы модалку, которую только что закрыли.
export function DailySummaryModal() {
  const { data } = useGetDailySummaryQuery();
  const [markSeen] = useMarkDailySummarySeenMutation();
  const [dismissed, setDismissed] = useState(false);

  const open = Boolean(data?.shouldShow) && !dismissed;

  const handleClose = () => {
    setDismissed(true);
    if (data?.date) {
      markSeen({ date: data.date }).catch(() => {
        // Не показать сводку второй раз важнее, чем гарантировать серверную
        // отметку: markSeen идемпотентна, следующий визит попробует снова.
      });
    }
  };

  if (!data) return null;

  return (
    <Dialog.Root open={open} onOpenChange={(details) => !details.open && handleClose()}>
      <Portal>
        <Dialog.Backdrop />
        <Dialog.Positioner>
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Итоги дня</Dialog.Title>
            </Dialog.Header>
            <Dialog.Body>
              <Stack gap="3">
                <Text fontSize="lg" fontWeight="semibold">
                  +{data.xpTotal ?? 0} XP
                </Text>
                <Stack gap="1">
                  {(data.breakdown ?? []).map((row) => (
                    <Text key={row.key} color="fg.muted">
                      {row.label} × {row.count} — {row.xp} XP
                    </Text>
                  ))}
                </Stack>
                {data.pet?.levelAfter !== undefined && data.pet.levelBefore !== undefined && (
                  <Text
                    color={data.pet.levelAfter > data.pet.levelBefore ? 'fg.success' : 'fg.muted'}
                  >
                    {data.pet.levelAfter > data.pet.levelBefore
                      ? `Новый уровень: ${data.pet.levelAfter}!`
                      : `Уровень: ${data.pet.levelAfter}`}
                  </Text>
                )}
                {data.aiNote && (
                  <Text fontStyle="italic" color="fg.muted">
                    «{data.aiNote}»
                  </Text>
                )}
              </Stack>
            </Dialog.Body>
            <Dialog.Footer>
              <Button
                colorPalette="blue"
                onClick={() => {
                  handleClose();
                }}
              >
                Понятно
              </Button>
            </Dialog.Footer>
            <Dialog.CloseTrigger />
          </Dialog.Content>
        </Dialog.Positioner>
      </Portal>
    </Dialog.Root>
  );
}
