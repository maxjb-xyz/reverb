import '@testing-library/jest-dom'
// jsdom does not implement media playback — stub so the AudioEngine singleton works under test.
if (typeof window !== 'undefined' && window.HTMLMediaElement) {
  window.HTMLMediaElement.prototype.play = async () => {}
  window.HTMLMediaElement.prototype.pause = () => {}
  window.HTMLMediaElement.prototype.load = () => {}
}
