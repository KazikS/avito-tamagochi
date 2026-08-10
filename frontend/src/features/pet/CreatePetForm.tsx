import { useState } from 'react';
import {
  Button,
  Card,
  Field,
  Heading,
  HStack,
  Input,
  RadioGroup,
  Stack,
  Text,
} from '@chakra-ui/react';
import type { PresetId } from '@/shared/types';
import { useCreatePetMutation } from './api';
import { toaster } from '@/shared/ui/theme/toaster-instance';

// Четыре круга Авито как персонажа рисовать нельзя (AGENTS.md → Never) —
// пресеты различаются подписью и акцентным цветом чипа, не логотипом.
const PRESETS: { id: PresetId; label: string; color: string }[] = [
  { id: 'green', label: 'Зелёный', color: 'green.solid' },
  { id: 'blue', label: 'Синий', color: 'blue.solid' },
  { id: 'purple', label: 'Фиолетовый', color: 'purple.solid' },
];

export function CreatePetForm() {
  const [presetId, setPresetId] = useState<PresetId>('green');
  const [name, setName] = useState('');
  const [createPet, { isLoading }] = useCreatePetMutation();

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    createPet({ presetId, name: name.trim() || undefined })
      .unwrap()
      .catch(() => {
        toaster.create({ type: 'error', title: 'Не удалось создать питомца' });
      });
  };

  return (
    <Card.Root maxW="md" mx="auto">
      <Card.Header>
        <Heading size="md">Заведи питомца</Heading>
        <Text color="fg.muted">Пресет и имя выбираются один раз — питомец на аккаунт один.</Text>
      </Card.Header>
      <Card.Body>
        <form onSubmit={handleSubmit}>
          <Stack gap="4">
            <Field.Root>
              <Field.Label>Пресет</Field.Label>
              <RadioGroup.Root
                value={presetId}
                onValueChange={(details) => {
                  if (details.value) setPresetId(details.value as PresetId);
                }}
              >
                <HStack gap="4">
                  {PRESETS.map((preset) => (
                    <RadioGroup.Item key={preset.id} value={preset.id}>
                      <RadioGroup.ItemHiddenInput />
                      <RadioGroup.ItemIndicator />
                      <RadioGroup.ItemText>{preset.label}</RadioGroup.ItemText>
                    </RadioGroup.Item>
                  ))}
                </HStack>
              </RadioGroup.Root>
            </Field.Root>

            <Field.Root>
              <Field.Label>Имя (необязательно)</Field.Label>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Ави"
                maxLength={40}
              />
            </Field.Root>

            <Button type="submit" loading={isLoading} colorPalette="blue">
              Создать питомца
            </Button>
          </Stack>
        </form>
      </Card.Body>
    </Card.Root>
  );
}
