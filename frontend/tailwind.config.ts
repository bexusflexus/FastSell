import type { Config } from 'tailwindcss';

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        graphite: {
          950: '#050606',
          900: '#0d0f0e',
          850: '#161a18',
          800: '#242a26',
          700: '#343c36',
        },
        amberline: {
          500: '#c5361f',
          400: '#f0522a',
          300: '#ff7a38',
          200: '#ffb06f',
          100: '#ffd8ad',
        },
        copper: {
          600: '#642921',
          500: '#8c3729',
          400: '#c95735',
        },
        signal: {
          red: '#ff3b24',
          green: '#8bcf8b',
        },
        rack: {
          steel: '#565d54',
          trim: '#2b332e',
          soot: '#080908',
          glass: '#a97958',
        },
      },
      boxShadow: {
        panel: '0 24px 70px rgba(0, 0, 0, 0.62), inset 0 1px 0 rgba(255, 122, 56, 0.045)',
        glow: '0 0 22px rgba(255, 60, 28, 0.38), 0 0 5px rgba(255, 126, 54, 0.45)',
      },
      fontFamily: {
        display: ['Inter', 'ui-sans-serif', 'system-ui', 'sans-serif'],
      },
    },
  },
  plugins: [],
} satisfies Config;
