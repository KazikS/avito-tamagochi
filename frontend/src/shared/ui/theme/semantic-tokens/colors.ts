import { defineSemanticTokens } from '@chakra-ui/react';

export const colors = defineSemanticTokens.colors({
  bg: {
    canvas: { value: { base: '#F1F2F0', _dark: '#0F1114' } },
    surface: { value: { base: '#FFFFFF', _dark: '#171A20' } },
    subtle: { value: { base: '#F7F7F6', _dark: '#1D2128' } },
    muted: { value: { base: '#ECEDEA', _dark: '#262C36' } },
    inverse: { value: { base: '#0F1114', _dark: '#262C36' } },
    scrim: { value: { base: 'rgba(10, 12, 15, 0.42)', _dark: 'rgba(0, 0, 0, 0.62)' } },
  },

  fg: {
    DEFAULT: { value: { base: '#0F1114', _dark: '#F4F5F3' } },
    muted: { value: { base: '#3A3D42', _dark: '#C9CCD2' } },
    subtle: { value: { base: '#767A80', _dark: '#9299A1' } },
    faint: { value: { base: '#9CA0A6', _dark: '#71777F' } },
    inverse: { value: { base: '#FFFFFF', _dark: '#F4F5F3' } },
    onAccent: { value: { base: '#06301A', _dark: '#06301A' } },
  },

  border: {
    DEFAULT: { value: { base: '#E3E4E1', _dark: '#2A2F37' } },
    subtle: { value: { base: '#EDEEEB', _dark: '#22272E' } },
    strong: { value: { base: '#0F1114', _dark: '#F4F5F3' } },
  },

  accent: {
    solid: { value: { base: '#04E061', _dark: '#04E061' } },
    fg: { value: { base: '#00873C', _dark: '#5CEFA0' } },
    subtle: { value: { base: '#E8FCF1', _dark: '#08301C' } },
    muted: { value: { base: '#C6F8DE', _dark: '#0E4A2C' } },
  },

  stat: {
    hunger: {
      solid: { value: { base: '#04E061', _dark: '#04E061' } },
      fg: { value: { base: '#00873C', _dark: '#5CEFA0' } },
      subtle: { value: { base: '#E8FCF1', _dark: '#08301C' } },
    },
    joy: {
      solid: { value: { base: '#965EEB', _dark: '#965EEB' } },
      fg: { value: { base: '#6E3ACB', _dark: '#C1A0FF' } },
      subtle: { value: { base: '#F3EDFE', _dark: '#251743' } },
    },
    clean: {
      solid: { value: { base: '#00AAFF', _dark: '#00AAFF' } },
      fg: { value: { base: '#0072B8', _dark: '#6BC8FF' } },
      subtle: { value: { base: '#E5F6FF', _dark: '#06283D' } },
    },
    energy: {
      solid: { value: { base: '#FF4053', _dark: '#FF4053' } },
      fg: { value: { base: '#D6203A', _dark: '#FF8894' } },
      subtle: { value: { base: '#FFECEE', _dark: '#3A1218' } },
    },
  },

  success: {
    solid: { value: { base: '#04E061', _dark: '#04E061' } },
    fg: { value: { base: '#00873C', _dark: '#5CEFA0' } },
    subtle: { value: { base: '#E8FCF1', _dark: '#08301C' } },
    muted: { value: { base: '#C6F8DE', _dark: '#0E4A2C' } },
    onSolid: { value: { base: '#06301A', _dark: '#06301A' } },
  },
  danger: {
    solid: { value: { base: '#FF4053', _dark: '#FF4053' } },
    fg: { value: { base: '#D6203A', _dark: '#FF8894' } },
    subtle: { value: { base: '#FFECEE', _dark: '#3A1218' } },
    muted: { value: { base: '#FFD3D8', _dark: '#5A1E27' } },
    onSolid: { value: { base: '#3A0710', _dark: '#3A0710' } },
  },
  warning: {
    solid: { value: { base: '#FF9500', _dark: '#FF9500' } },
    fg: { value: { base: '#8F5300', _dark: '#FFC46B' } },
    subtle: { value: { base: '#FFF4E3', _dark: '#3A2200' } },
    muted: { value: { base: '#FBE3B8', _dark: '#573600' } },
    onSolid: { value: { base: '#3A2200', _dark: '#3A2200' } },
  },
  info: {
    solid: { value: { base: '#00AAFF', _dark: '#00AAFF' } },
    fg: { value: { base: '#0072B8', _dark: '#6BC8FF' } },
    subtle: { value: { base: '#E5F6FF', _dark: '#06283D' } },
    muted: { value: { base: '#C0EAFF', _dark: '#0A3E5C' } },
    onSolid: { value: { base: '#04202F', _dark: '#04202F' } },
  },

  streak: {
    flame: { value: { base: '#FF4053', _dark: '#FF4053' } },
    freeze: { value: { base: '#00AAFF', _dark: '#00AAFF' } },
  },

  rarity: {
    common: { value: { base: '#767A80', _dark: '#9299A1' } },
    rare: { value: { base: '#0072B8', _dark: '#6BC8FF' } },
    legendary: { value: { base: '#6E3ACB', _dark: '#C1A0FF' } },
  },
});
