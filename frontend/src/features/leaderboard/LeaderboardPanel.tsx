import { Badge, Card, Heading, Spinner, Stack, Text } from '@chakra-ui/react';
import { useGetLeaderboardQuery } from './api';

export function LeaderboardPanel() {
  const { data, isLoading, error } = useGetLeaderboardQuery();

  return (
    <Card.Root maxW="lg" mx="auto">
      <Card.Header>
        <Heading size="md">Лидерборд недели</Heading>
        <Text color="fg.muted">Топ по опыту за 7 дней</Text>
      </Card.Header>
      <Card.Body>
        {isLoading && <Spinner />}
        {error && <Text color="fg.error">Не удалось загрузить лидерборд</Text>}
        {data && (
          <Stack gap="2">
            {(data.items ?? []).map((entry) => (
              <Stack key={entry.userId} direction="row" justify="space-between" align="center">
                <Stack direction="row" gap="2" align="center">
                  <Text fontWeight="semibold">#{entry.rank}</Text>
                  <Text>{entry.nickname}</Text>
                  {entry.isMe && <Badge colorPalette="blue">Вы</Badge>}
                </Stack>
                <Text color="fg.muted">{entry.weeklyXp} XP</Text>
              </Stack>
            ))}
            {(data.items ?? []).length === 0 && (
              <Text color="fg.muted">Пока никто не набрал опыта за неделю</Text>
            )}
            {data.me && !(data.items ?? []).some((entry) => entry.isMe) && (
              <Stack
                direction="row"
                justify="space-between"
                align="center"
                borderTopWidth="1px"
                pt="2"
              >
                <Stack direction="row" gap="2" align="center">
                  <Text fontWeight="semibold">#{data.me.rank}</Text>
                  <Text>{data.me.nickname}</Text>
                  <Badge colorPalette="blue">Вы</Badge>
                </Stack>
                <Text color="fg.muted">{data.me.weeklyXp} XP</Text>
              </Stack>
            )}
          </Stack>
        )}
      </Card.Body>
    </Card.Root>
  );
}
