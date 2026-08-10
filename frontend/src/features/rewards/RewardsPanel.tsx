import { Badge, Button, Card, Heading, Progress, Spinner, Stack, Text } from '@chakra-ui/react';
import type { Reward } from '@/shared/types';
import { useClaimRewardMutation, useGetRewardsQuery, useRedeemRewardMutation } from './api';
import { toaster } from '@/shared/ui/theme/toaster-instance';

const TIER_LABELS: Record<string, string> = {
  cosmetic: 'Косметика',
  soft: 'Бонус',
  premium: 'Премиум',
};

function RewardRow({ reward }: { reward: Reward }) {
  const [claim, { isLoading: isClaiming }] = useClaimRewardMutation();
  const [redeem, { isLoading: isRedeeming }] = useRedeemRewardMutation();

  const handleClaim = () => {
    claim({ rewardId: reward.id })
      .unwrap()
      .then(() => {
        toaster.create({ type: 'success', title: `Награда получена: ${reward.title}` });
      })
      .catch((err: unknown) => {
        const message =
          err && typeof err === 'object' && 'message' in err
            ? String(err.message)
            : 'Не удалось получить награду';
        toaster.create({ type: 'error', title: message });
      });
  };

  const handleRedeem = () => {
    redeem({ rewardId: reward.id })
      .unwrap()
      .catch(() => {
        toaster.create({ type: 'error', title: 'Не удалось применить награду' });
      });
  };

  return (
    <Card.Root variant="subtle">
      <Card.Body>
        <Stack gap="2">
          <Stack direction="row" justify="space-between" align="center">
            <Text fontWeight="semibold">{reward.title}</Text>
            <Badge colorPalette={reward.tier === 'cosmetic' ? 'purple' : 'blue'}>
              {TIER_LABELS[reward.tier] ?? reward.tier}
            </Badge>
          </Stack>
          {reward.description && (
            <Text color="fg.muted" fontSize="sm">
              {reward.description}
            </Text>
          )}
          <Progress.Root
            value={reward.progress.current}
            max={reward.progress.target}
            colorPalette="green"
            size="sm"
          >
            <Progress.Label>{reward.conditionLabel}</Progress.Label>
            <Progress.Track>
              <Progress.Range />
            </Progress.Track>
          </Progress.Root>

          {reward.status === 'locked' && (
            <Button size="sm" disabled>
              Пока недоступно
            </Button>
          )}
          {reward.status === 'available' && (
            <Button size="sm" colorPalette="blue" onClick={handleClaim} loading={isClaiming}>
              Забрать
            </Button>
          )}
          {reward.status === 'claimed' && (
            <Button size="sm" colorPalette="green" onClick={handleRedeem} loading={isRedeeming}>
              Применить
            </Button>
          )}
          {reward.status === 'used' && (
            <Badge colorPalette="green" alignSelf="start">
              Применена
            </Badge>
          )}
        </Stack>
      </Card.Body>
    </Card.Root>
  );
}

export function RewardsPanel() {
  const { data, isLoading, error } = useGetRewardsQuery();

  return (
    <Card.Root maxW="lg" mx="auto">
      <Card.Header>
        <Heading size="md">Награды</Heading>
        <Text color="fg.muted">Бонусы на реальные услуги Авито — открываются по уровню Ави</Text>
      </Card.Header>
      <Card.Body>
        {isLoading && <Spinner />}
        {error && <Text color="fg.error">Не удалось загрузить награды</Text>}
        {data && (
          <Stack gap="3">
            {data.map((reward) => (
              <RewardRow key={reward.id} reward={reward} />
            ))}
          </Stack>
        )}
      </Card.Body>
    </Card.Root>
  );
}
