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
          from: { opacity: '0', transform: 'translateY(6px) scale(.95)' },
          to: { opacity: '1', transform: 'scale(1)' },
        },
        'slide-in-right': {
          from: { opacity: '0', transform: 'translateX(16px)' },
          to: { opacity: '1', transform: 'translateX(0)' },
        },
        'rise-in': {
          from: { opacity: '0', transform: 'translateY(10px)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        },
        'toast-in': {
          from: { opacity: '0', transform: 'translateY(12px) scale(.96)' },
          to: { opacity: '1', transform: 'translateY(0) scale(1)' },
        },
        'menu-in': {
          from: { opacity: '0', transform: 'translateY(-4px) scale(.96)' },
          to: { opacity: '1', transform: 'translateY(0) scale(1)' },
        },
        pop: {
          '0%': { transform: 'scale(.6)', opacity: '0' },
          '60%': { transform: 'scale(1.15)', opacity: '1' },
          '100%': { transform: 'scale(1)' },
        },
      },
      animation: {
        eq: 'eq .9s ease-in-out infinite',
        'fade-in': 'fade-in .2s ease-out',
        'scale-in': 'scale-in .26s cubic-bezier(.22,1,.36,1)',
        'slide-in-right': 'slide-in-right .2s cubic-bezier(.2,.8,.2,1)',
        'rise-in': 'rise-in .36s cubic-bezier(.22,1,.36,1) backwards',
        'toast-in': 'toast-in .32s cubic-bezier(.22,1,.36,1)',
        'menu-in': 'menu-in .18s cubic-bezier(.22,1,.36,1)',
        pop: 'pop .34s cubic-bezier(.34,1.56,.64,1)',
      },
      transitionTimingFunction: {
        smooth: 'cubic-bezier(.22,1,.36,1)',
        spring: 'cubic-bezier(.34,1.56,.64,1)',
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
        lift: '0 18px 36px -12px rgba(0,0,0,.75)',
        glow: '0 8px 28px -6px rgb(var(--color-accent) / .55)',
      },
    },
  },
  plugins: [],
}
