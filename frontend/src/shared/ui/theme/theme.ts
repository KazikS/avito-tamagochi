import { defineConfig, createSystem, defaultConfig } from '@chakra-ui/react';
import { semanticTokens } from './semantic-tokens';
import { tokens } from './tokens';

export const config = defineConfig({
  globalCss: {
    '.mono, [data-mono]': {
      fontFamily: 'numbers',
      fontFeatureSettings: '"tnum"',
    },
  },
  theme: {
    semanticTokens,
    tokens,
  },
});

export const system = createSystem(defaultConfig, config);
