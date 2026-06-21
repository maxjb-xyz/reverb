/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      keyframes: {
        eq: {
          '0%, 100%': { height: '4px' },
          '50%': { height: '14px' },
        },
      },
      animation: {
        eq: 'eq .9s ease-in-out infinite',
      },
      colors: {
        accent: 'rgb(var(--color-accent) / <alpha-value>)',
        base: 'var(--bg-base)',
        surface: 'var(--bg-surface)',
        raised: 'var(--bg-raised)',
        'raised-hover': 'var(--bg-raised-hover)',
        input: 'var(--bg-input)',
        'border-subtle': 'var(--border-subtle)',
        'text-primary': 'var(--text-primary)',
        'text-secondary': 'var(--text-secondary)',
        'text-muted': 'var(--text-muted)',
        success: 'var(--status-success)',
        warning: 'var(--status-warning)',
        error: 'var(--status-error)',
      },
      fontFamily: {
        sans: ['"Figtree Variable"', 'Figtree', 'system-ui', '-apple-system', 'sans-serif'],
      },
      boxShadow: {
        cover: '0 8px 18px -8px rgba(0,0,0,.6)',
        float: '0 8px 16px rgba(0,0,0,.35)',
        pop: '0 24px 60px rgba(0,0,0,.6)',
      },
    },
  },
  plugins: [],
}
