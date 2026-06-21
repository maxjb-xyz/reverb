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
        'fade-in': {
          from: { opacity: '0' },
          to: { opacity: '1' },
        },
        'scale-in': {
          from: { opacity: '0', transform: 'scale(.96)' },
          to: { opacity: '1', transform: 'scale(1)' },
        },
        'slide-in-right': {
          from: { opacity: '0', transform: 'translateX(16px)' },
          to: { opacity: '1', transform: 'translateX(0)' },
        },
      },
      animation: {
        eq: 'eq .9s ease-in-out infinite',
        'fade-in': 'fade-in .15s ease-out',
        'scale-in': 'scale-in .16s cubic-bezier(.2,.8,.2,1)',
        'slide-in-right': 'slide-in-right .2s cubic-bezier(.2,.8,.2,1)',
      },
      colors: {
        accent: 'rgb(var(--color-accent) / <alpha-value>)',
        'on-accent': 'rgb(var(--on-accent) / <alpha-value>)',
        // NOTE: do not name this 'base' — a `base` color makes Tailwind emit a
        // `text-base` color utility that collides with the built-in `text-base`
        // font-size utility used to size icons, turning accent icons black.
        canvas: 'var(--bg-base)',
        surface: 'var(--bg-surface)',
        raised: 'var(--bg-raised)',
        'raised-hover': 'var(--bg-raised-hover)',
        input: 'var(--bg-input)',
        'border-subtle': 'var(--border-subtle)',
        'text-primary': 'var(--text-primary)',
        'text-secondary': 'var(--text-secondary)',
        'text-muted': 'var(--text-muted)',
        success: 'rgb(var(--status-success) / <alpha-value>)',
        warning: 'rgb(var(--status-warning) / <alpha-value>)',
        error: 'rgb(var(--status-error) / <alpha-value>)',
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
