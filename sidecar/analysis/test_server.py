"""Tests for the HTTP side of server.py with a stand-in model (no torch needed).

Run: python3 -m unittest test_server.py
"""

import json
import threading
import unittest
import urllib.error
import urllib.request
import uuid
from http.server import ThreadingHTTPServer

import server


class FakeModel:
    name = "fake-v1"
    device = "cpu"

    def __init__(self):
        self.seen = []

    def load(self):
        pass

    def analyze(self, path):
        with open(path, "rb") as f:
            data = f.read()
        self.seen.append((path, data))
        if data == b"broken":
            raise ValueError("not audio")
        return [0.1] * 512, 120.0, 0.5


def multipart(field, filename, data):
    boundary = uuid.uuid4().hex
    body = (
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"{field}\"; filename=\"{filename}\"\r\n"
        f"Content-Type: application/octet-stream\r\n\r\n"
    ).encode() + data + f"\r\n--{boundary}--\r\n".encode()
    return body, f"multipart/form-data; boundary={boundary}"


class ServerTest(unittest.TestCase):
    def start(self, api_key="", ready=True):
        self.model = FakeModel()
        self.svc = server.Service(self.model, api_key=api_key, max_bytes=1 << 20)
        if ready:
            self.svc.load()
        self.httpd = ThreadingHTTPServer(("127.0.0.1", 0), server.make_handler(self.svc))
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()
        self.addCleanup(self.httpd.shutdown)
        self.base = f"http://127.0.0.1:{self.httpd.server_address[1]}"

    def call(self, method, path, body=None, ctype=None, key=None):
        req = urllib.request.Request(self.base + path, data=body, method=method)
        if ctype:
            req.add_header("Content-Type", ctype)
        if key:
            req.add_header("Authorization", f"Bearer {key}")
        try:
            with urllib.request.urlopen(req) as r:
                return r.status, json.loads(r.read())
        except urllib.error.HTTPError as e:
            return e.code, json.loads(e.read())

    def test_health_and_analyze(self):
        self.start(api_key="k")
        self.assertEqual(self.call("GET", "/v1/health")[0], 401)
        code, body = self.call("GET", "/v1/health", key="k")
        self.assertEqual((code, body["model"]), (200, "fake-v1"))
        data, ctype = multipart("audio", "song.flac", b"\x00\x01audio")
        code, body = self.call("POST", "/v1/analyze", data, ctype, key="k")
        self.assertEqual(code, 200, body)
        self.assertEqual(len(body["embedding"]), 512)
        self.assertEqual(body["tempo"], 120.0)
        self.assertEqual(self.model.seen[0][1], b"\x00\x01audio")
        self.assertTrue(self.model.seen[0][0].endswith(".flac"))

    def test_loading_bad_field_and_bad_audio(self):
        self.start(ready=False)
        data, ctype = multipart("audio", "a.mp3", b"x")
        self.assertEqual(self.call("POST", "/v1/analyze", data, ctype)[0], 503)
        self.assertEqual(self.call("GET", "/v1/health")[0], 503)
        self.svc.load()
        data, ctype = multipart("file", "a.mp3", b"x")
        self.assertEqual(self.call("POST", "/v1/analyze", data, ctype)[0], 400)
        data, ctype = multipart("audio", "a.mp3", b"broken")
        self.assertEqual(self.call("POST", "/v1/analyze", data, ctype)[0], 422)
        data, ctype = multipart("audio", "a.mp3", b"x" * (2 << 20))
        try:
            code = self.call("POST", "/v1/analyze", data, ctype)[0]
        except (urllib.error.URLError, ConnectionError):
            code = 413  # the server answered 413 and dropped the upload mid-send
        self.assertEqual(code, 413)

    def test_chunked_upload(self):
        # Reverb streams songs with Transfer-Encoding: chunked.
        self.start()
        data, ctype = multipart("audio", "song.mp3", b"chunked-audio" * 1000)

        def chunks():
            for i in range(0, len(data), 4096):
                yield data[i:i + 4096]

        req = urllib.request.Request(self.base + "/v1/analyze", data=chunks(), method="POST")
        req.add_header("Content-Type", ctype)
        req.add_header("Transfer-Encoding", "chunked")
        with urllib.request.urlopen(req) as r:
            body = json.loads(r.read())
        self.assertEqual(body["model"], "fake-v1")
        self.assertEqual(self.model.seen[0][1], b"chunked-audio" * 1000)

    def test_energy_from_rms(self):
        self.assertEqual(server.energy_from_rms(0), 0.0)
        self.assertEqual(server.energy_from_rms(1.0), 1.0)
        self.assertAlmostEqual(server.energy_from_rms(10 ** (-23 / 20)), 0.5, places=2)


if __name__ == "__main__":
    unittest.main()
