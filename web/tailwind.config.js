/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        gem: {
          white: '#f4f1ea',
          blue: '#2f6fd6',
          green: '#2e9e5b',
          red: '#d64045',
          black: '#3a3236',
          gold: '#e8b923',
        },
      },
    },
  },
  plugins: [],
}
