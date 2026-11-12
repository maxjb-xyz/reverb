"""Reverb's reference audio-analysis service.

Reverb sends each library song here (POST /v1/analyze, multipart field
"audio") and gets back an embedding where songs that sound alike sit close
together, plus tempo and energy. It runs LAION-CLAP's music checkpoint, which
is much faster on a GPU but works on a CPU.

Stdlib HTTP so the only heavy dependencies are the model's own. Configure with
environment variables:

  ANALYSIS_PORT      port to listen on (default 8765)
  ANALYSIS_API_KEY   if set, requests need "Authorization: Bearer <key>"
  ANALYSIS_DEVICE    cuda, cpu or auto (default auto)
  ANALYSIS_MAX_MB    largest upload accepted, in MB (default 200)
"""

import email.parser
import email.policy
import hmac
import json
import math
import os
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

SAMPLE_RATE = 48000
WINDOW_SECONDS = 10
# Where in the song the windows are taken (fractions of its length).
WINDOW_AT = (0.25, 0.5, 0.75)


class ClapModel:
    """LAION-CLAP (music checkpoint). Heavy imports happen in load()."""

    name = "laion-clap-music-v1+3x10s"

    def __init__(self, device="auto"):
        self.device = device
        self.model = None

    def load(self):
        import torch
        import laion_clap

        if self.device == "auto":
            self.device = "cuda" if torch.cuda.is_available() else "cpu"
        model = laion_clap.CLAP_Module(enable_fusion=False, amodel="HTSAT-base", device=self.device)
        # Downloads music_audioset_epoch_15_esc_90.14.pt (about 2 GB) on first run.
        model.load_ckpt(model_id=None, ckpt=_checkpoint())
        model.eval()
        self.model = model

    def analyze(self, path):
        import librosa
        import numpy as np
        import torch

        y, _ = librosa.load(path, sr=SAMPLE_RATE, mono=True)
        if y.size < SAMPLE_RATE:
            raise ValueError("the song is shorter than a second")
        win = WINDOW_SECONDS * SAMPLE_RATE
        windows = []
        for at in WINDOW_AT:
            start = max(0, min(int(y.size * at) - win // 2, y.size - win))
            seg = y[start:start + win]
            if seg.size < win:
                seg = np.pad(seg, (0, win - seg.size))
            windows.append(seg)
        batch = np.stack(windows).astype(np.float32)
        with torch.no_grad():
            emb = self.model.get_audio_embedding_from_data(x=batch, use_tensor=False)
        vec = np.asarray(emb, dtype=np.float64).mean(axis=0)
        tempo, _ = librosa.beat.beat_track(y=y[: 120 * SAMPLE_RATE], sr=SAMPLE_RATE)
        tempo = float(np.atleast_1d(tempo)[0])
        rms = float(np.sqrt(np.mean(np.square(y))))
        return vec.tolist(), tempo, energy_from_rms(rms)


def _checkpoint():
    from huggingface_hub import hf_hub_download

    return hf_hub_download("lukewys/laion_clap", "music_audioset_epoch_15_esc_90.14.pt")


def energy_from_rms(rms):
    """Map loudness to 0..1: -40 dBFS and below is 0, -6 dBFS and above is 1."""
    if rms <= 0:
        return 0.0
    db = 20 * math.log10(rms)
    return max(0.0, min(1.0, (db + 40) / 34))


class Service:
    def __init__(self, model, api_key="", max_bytes=200 << 20):
        self.model = model
        self.api_key = api_key
        self.max_bytes = max_bytes
        self.ready = threading.Event()
        self.error = ""
        # One song at a time: the model (and a GPU) is the bottleneck anyway.
        self.lock = threading.Lock()

    def load(self):
        try:
            self.model.load()
            self.ready.set()
            print(f"analysis: {self.model.name} ready on {getattr(self.model, 'device', '?')}", flush=True)
        except Exception as e:  # noqa: BLE001 - report any load failure on /v1/health
            self.error = f"model failed to load: {e}"
            print(f"analysis: {self.error}", file=sys.stderr, flush=True)


def make_handler(svc):
    class Handler(BaseHTTPRequestHandler):
        server_version = "reverb-analysis/1"

        def log_message(self, fmt, *args):  # quieter than the default
            if os.environ.get("ANALYSIS_VERBOSE"):
                super().log_message(fmt, *args)

        def _json(self, code, body):
            data = json.dumps(body).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def _authorized(self):
            if not svc.api_key:
                return True
            got = self.headers.get("Authorization", "")
            return hmac.compare_digest(got.encode(), f"Bearer {svc.api_key}".encode())

        def do_GET(self):
            if not self._authorized():
                return self._json(401, {"error": "unauthorized"})
            if self.path != "/v1/health":
                return self._json(404, {"error": "not found"})
            if svc.error:
                return self._json(500, {"error": svc.error})
            if not svc.ready.is_set():
                return self._json(503, {"error": "loading"})
            return self._json(200, {"model": svc.model.name, "device": getattr(svc.model, "device", "")})

        def do_POST(self):
            if not self._authorized():
                return self._json(401, {"error": "unauthorized"})
            if self.path != "/v1/analyze":
                return self._json(404, {"error": "not found"})
            if not svc.ready.is_set():
                return self._json(503, {"error": svc.error or "loading"})
            body, err = read_body(self.headers, self.rfile, svc.max_bytes)
            if err:
                # Don't read the rest of an oversized upload; drop the connection.
                self.close_connection = True
                return self._json(err[0], {"error": err[1]})
            audio, name = parse_upload(self.headers.get("Content-Type", ""), body)
            if audio is None:
                return self._json(400, {"error": 'expected a multipart field named "audio"'})
            suffix = os.path.splitext(name or "")[1][:6] or ".audio"
            with tempfile.NamedTemporaryFile(suffix=suffix) as f:
                f.write(audio)
                f.flush()
                try:
                    with svc.lock:
                        emb, tempo, energy = svc.model.analyze(f.name)
                except Exception as e:  # noqa: BLE001 - one bad file must not stop the service
                    return self._json(422, {"error": f"could not analyze: {e}"})
            if not emb or any(not math.isfinite(v) for v in emb):
                return self._json(500, {"error": "the model returned an empty or invalid embedding"})
            if not math.isfinite(tempo) or tempo < 0 or tempo > 400:
                tempo = 0.0
            return self._json(200, {"model": svc.model.name, "embedding": emb, "tempo": tempo, "energy": energy})

    return Handler


def read_body(headers, rfile, max_bytes):
    """Read a request body sent with Content-Length or chunked (Reverb streams
    songs chunked). Returns (body, None) or (None, (status, message))."""
    if headers.get("Transfer-Encoding", "").lower() == "chunked":
        parts, total = [], 0
        while True:
            line = rfile.readline(1024)
            try:
                size = int(line.split(b";", 1)[0].strip(), 16)
            except ValueError:
                return None, (400, "bad chunked body")
            if size == 0:
                # Trailers, then the blank line that ends the body.
                while rfile.readline(1024) not in (b"\r\n", b"\n", b""):
                    pass
                return b"".join(parts), None
            total += size
            if total > max_bytes:
                return None, (413, "song too large")
            parts.append(rfile.read(size))
            rfile.readline(16)  # the CRLF after each chunk
    try:
        length = int(headers.get("Content-Length", "0"))
    except ValueError:
        length = 0
    if length <= 0:
        return None, (411, "send the song with a Content-Length or chunked")
    if length > max_bytes:
        return None, (413, "song too large")
    return rfile.read(length), None


def parse_upload(content_type, body):
    """Return (bytes, filename) of the "audio" part, or (None, None)."""
    if not content_type.startswith("multipart/form-data"):
        return None, None
    msg = email.parser.BytesParser(policy=email.policy.HTTP).parsebytes(
        b"Content-Type: " + content_type.encode() + b"\r\n\r\n" + body
    )
    if not msg.is_multipart():
        return None, None
    for part in msg.iter_parts():
        if part.get_param("name", header="content-disposition") == "audio":
            return part.get_payload(decode=True), part.get_filename()
    return None, None


def main():
    port = int(os.environ.get("ANALYSIS_PORT", "8765"))
    svc = Service(
        ClapModel(os.environ.get("ANALYSIS_DEVICE", "auto")),
        api_key=os.environ.get("ANALYSIS_API_KEY", ""),
        max_bytes=int(os.environ.get("ANALYSIS_MAX_MB", "200")) << 20,
    )
    threading.Thread(target=svc.load, daemon=True).start()
    httpd = ThreadingHTTPServer(("0.0.0.0", port), make_handler(svc))
    print(f"analysis: listening on :{port} (loading the model in the background)", flush=True)
    httpd.serve_forever()


if __name__ == "__main__":
    main()
