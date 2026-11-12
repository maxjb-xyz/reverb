"""Reverb's built-in DJ voice: a Kokoro-82M worker.

Reverb starts this script and talks to it over stdin/stdout, one JSON object
per line. On start it loads the model and prints {"ready": true, "voices": [...]}.
Each request {"text": str, "voice": str, "speed": float} gets one answer line:
{"ok": true, "audio": <base64 WAV>} or {"ok": false, "error": str}.
It exits when stdin closes.
"""

import argparse
import base64
import io
import json
import sys
import wave


def wav_bytes(samples, rate):
    import numpy as np

    pcm = (np.clip(samples, -1.0, 1.0) * 32767).astype("<i2").tobytes()
    buf = io.BytesIO()
    with wave.open(buf, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(rate)
        w.writeframes(pcm)
    return buf.getvalue()


def reply(obj):
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", required=True)
    ap.add_argument("--voices", required=True)
    args = ap.parse_args()

    from kokoro_onnx import Kokoro

    kokoro = Kokoro(args.model, args.voices)
    voices = sorted(kokoro.get_voices())
    reply({"ready": True, "voices": voices})

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
            voice = req.get("voice") or "af_heart"
            if voice not in voices:
                raise ValueError("unknown voice %r" % voice)
            speed = float(req.get("speed") or 1.0)
            # The first letter of a Kokoro voice names its language.
            lang = {"a": "en-us", "b": "en-gb", "e": "es", "f": "fr-fr", "h": "hi",
                    "i": "it", "j": "ja", "p": "pt-br", "z": "cmn"}.get(voice[0], "en-us")
            samples, rate = kokoro.create(req["text"], voice=voice, speed=speed, lang=lang)
            reply({"ok": True, "audio": base64.b64encode(wav_bytes(samples, rate)).decode()})
        except Exception as e:  # noqa: BLE001 - every failure goes back to Reverb
            reply({"ok": False, "error": str(e)[:300]})


if __name__ == "__main__":
    main()
