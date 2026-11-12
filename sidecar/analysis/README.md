# Reverb audio analysis (optional)

Reverb recommends from tags, your listening and similar artists out of the
box. This optional service adds a sense of **sound**: it listens to each song
in your library once and returns an embedding, a list of numbers where songs
that sound alike sit close together. With it, radio and the "sounds like" lane
add songs that sound like the one you started from, even across artists and
genres the tags don't connect. Each pick says why: *Sounds like Kerala*.

It runs [LAION-CLAP](https://github.com/LAION-AI/CLAP) (CC0) with its music
checkpoint. It's heavy, which is why it's a separate container and off by
default:

| | GPU (e.g. RTX 3060) | CPU (8 cores) |
|---|---|---|
| Per song | well under a second | a few seconds |
| 10,000 songs | minutes | several hours, once |
| Memory | about 3 GB VRAM | about 3 GB RAM |

These are rough expectations, not measurements. Analysis happens once per
song (and again only if you switch models), in the background, one song at a
time.

## Run it

```sh
docker build -t reverb-analysis sidecar/analysis        # GPU image
# or, CPU only:
docker build -t reverb-analysis:cpu --build-arg BASE=python:3.11-slim --build-arg TORCH=cpu sidecar/analysis
```

See `docker-compose.example.yml` for a compose service with a GPU. The first
start downloads the model (about 2 GB) into the `/models` volume, and
`/v1/health` answers 503 until it's loaded.

Then in Reverb: **Settings → Providers → Audio analysis → Add analyzer →
Analysis service**, with the URL the service is reachable at (for example
`http://reverb-analysis:8765`). Progress shows under **Settings →
Recommendations**. Radio starts using it once 50 songs are analyzed.

| Variable | Default | |
|---|---|---|
| `ANALYSIS_PORT` | `8765` | |
| `ANALYSIS_API_KEY` | (none) | If set, Reverb must send it (the provider's API key field). |
| `ANALYSIS_DEVICE` | `auto` | `cuda`, `cpu` or `auto`. |
| `ANALYSIS_MAX_MB` | `200` | Largest song accepted. |

## What Reverb sends

Each song's audio file, one at a time, named only `song.<ext>` (never its
path). Nothing leaves the machines you run. Point the provider at a service
on your own network.

## Protocol

Any service that speaks this works, so you can swap in another model:

```
GET  /v1/health   200 {"model": "...", "device": "cuda"} | 503 while loading
POST /v1/analyze  multipart field "audio" (plain or chunked)
                  200 {"model": "...", "embedding": [float, ...], "tempo": 120.0, "energy": 0.7}
                  503 busy or loading (Reverb retries later)
                  422 the file couldn't be decoded (Reverb retries in a week)
```

`embedding` has 16 to 4096 values; Reverb normalizes it. `model` must change
whenever embeddings stop being comparable, so Reverb re-analyzes.

## Tests

```sh
cd sidecar/analysis && python3 -m unittest test_server.py
```

They cover the HTTP side with a stand-in model; they don't need torch.
