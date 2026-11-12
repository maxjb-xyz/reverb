# Reverb's reference audio-analysis service (LAION-CLAP, music checkpoint).
#
# GPU (default):  docker build -t reverb-analysis sidecar/analysis
# CPU only:       docker build -t reverb-analysis:cpu \
#                   --build-arg BASE=python:3.11-slim --build-arg TORCH=cpu sidecar/analysis
ARG BASE=pytorch/pytorch:2.4.1-cuda12.1-cudnn9-runtime
FROM ${BASE}

ARG TORCH=image
# ffmpeg decodes mp3/m4a/opus for librosa.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY requirements.txt .
RUN if [ "$TORCH" = "cpu" ]; then \
      pip install --no-cache-dir torch==2.4.1 torchvision==0.19.1 --index-url https://download.pytorch.org/whl/cpu; \
    fi \
 && pip install --no-cache-dir -r requirements.txt

COPY server.py .

# The model (about 2 GB) downloads here on first start; mount a volume to keep it.
ENV HF_HOME=/models \
    ANALYSIS_PORT=8765 \
    PYTHONUNBUFFERED=1
VOLUME /models
EXPOSE 8765

RUN useradd --create-home --uid 10001 analysis && mkdir -p /models && chown analysis /models
USER analysis

HEALTHCHECK --interval=30s --timeout=5s --start-period=10m \
  CMD python -c "import os,urllib.request; r=urllib.request.Request('http://127.0.0.1:8765/v1/health'); k=os.environ.get('ANALYSIS_API_KEY'); k and r.add_header('Authorization','Bearer '+k); urllib.request.urlopen(r)"

CMD ["python", "server.py"]
