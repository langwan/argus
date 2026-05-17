#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Paraformer Unified Voice Service
Supports: 1. Speech-to-Text (ASR) 2. SRT Subtitle Generation with Timestamps
Model: Paraformer-zh + FSMN-VAD + CT-Punc
Component Version: 2026.04.29
"""

__version__ = "2026.04.29"
__component__ = "SenseVoice"

import argparse
import os
import re
import sys
import time
import traceback
import uvicorn
from fastapi import FastAPI, File, Form, UploadFile, HTTPException
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel

# Fix PyInstaller --noconsole mode BUG
# Without console mode, sys.stdout/stderr/stdin are all None
# This causes uvicorn/logging various libraries to crash
if sys.stdout is None:
    sys.stdout = open(os.devnull, "w")
if sys.stderr is None:
    sys.stderr = open(os.devnull, "w")
if sys.stdin is None:
    sys.stdin = open(os.devnull, "r")

# PyInstaller packaging runtime compatibility
# 1. Change working directory to EXE location
# 2. Force model cache to user home directory to avoid temporary directory issues
if getattr(sys, "frozen", False):
    exe_dir = os.path.dirname(sys.executable)
    os.chdir(exe_dir)
    # Force model cache to user home directory (PyInstaller temp directory will be deleted)
    os.environ["MODELSCOPE_CACHE"] = os.path.expanduser("~/.cache/modelscope")
    print(f"PyInstaller Mode: Working directory={exe_dir}")
    print(f"Model cache directory: {os.environ['MODELSCOPE_CACHE']}")

# Fix torchaudio compatibility issues
# New version torchaudio(>=2.5) has two serious compatibility issues:
# 1. torchaudio.load() forces torchcodec by default, but almost no one has this package installed
# 2. torchaudio.transforms.Resample requires sample rate to be integer type
# We thoroughly fix these issues here
try:
    import torch
    import torchaudio
    import numpy as np

    # ==========================================
    # Solution 1: Completely replace torchaudio.load function (most reliable)
    # ==========================================
    def patched_load(filepath, **kwargs):
        """Use soundfile/librosa instead of torchaudio.load, completely bypass torchcodec"""
        import soundfile as sf

        # Read audio
        audio_np, sample_rate = sf.read(filepath)

        # Convert to (channels, samples) format, matching torchaudio
        if len(audio_np.shape) == 1:
            audio_np = audio_np.reshape(1, -1)
        else:
            audio_np = audio_np.T

        # Convert to torch tensor
        audio_tensor = torch.from_numpy(audio_np).float()
        return audio_tensor, sample_rate

    torchaudio.load = patched_load
    print(
        "Replaced torchaudio.load with soundfile backend (completely bypass torchcodec)"
    )

    # ==========================================
    # Solution 2: Fix Resample sample rate integer issue
    # ==========================================
    original_resample_init = torchaudio.transforms.Resample.__init__

    def patched_resample_init(self, orig_freq, new_freq, *args, **kwargs):
        orig_freq_int = int(round(float(orig_freq)))
        new_freq_int = int(round(float(new_freq)))
        return original_resample_init(
            self, orig_freq_int, new_freq_int, *args, **kwargs
        )

    torchaudio.transforms.Resample.__init__ = patched_resample_init
    print("Fixed torchaudio Resample sample rate integer issue")

    # ==========================================
    # Solution 3: Create mock torchcodec module (fallback)
    # ==========================================
    from unittest.mock import MagicMock

    sys.modules["torchcodec"] = MagicMock()
    sys.modules["torchcodec.decoders"] = MagicMock()
    print("Injected mock torchcodec module (fallback protection)")

except Exception as e:
    print(
        f"torchaudio compatibility patch partially failed (core functionality unaffected): {e}"
    )

# --- Environment Configuration ---
# Auto-detect ffmpeg path (if exists)
# Adapts to new directory structure for Go service: data/bin/
ffmpeg_dir = os.path.join(
    os.path.dirname(__file__), "data", "bin", "ffmpeg-8.1-essentials_build", "bin"
)
if os.path.exists(ffmpeg_dir):
    os.environ["PATH"] = ffmpeg_dir + os.pathsep + os.environ["PATH"]


app = FastAPI(title="Paraformer Voice API", version=__version__)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.get("/version")
async def get_version():
    return {
        "component": __component__,
        "version": __version__,
        "service": "SenseVoice ASR Service",
        "model": "Paraformer-zh + FSMN-VAD + CT-Punc",
    }


# --- Model Global Loading ---
# Use this single AutoModel instance to save memory and improve efficiency
voice_model = None
is_ready = False


def load_voice_model():
    global voice_model, is_ready
    if voice_model is None:
        print("Loading Paraformer model (ASR + VAD + PUNC)...")
        try:
            from funasr import AutoModel

            voice_model = AutoModel(
                model="paraformer-zh",  # Core recognition model
                vad_model="fsmn-vad",  # Silence detection for automatic sentence splitting
                punc_model="ct-punc",  # Must keep to prevent source code logic errors
                device="cpu",  # Change to "cuda:0" if GPU is available
                disable_update=True,
                sentence_timestamp=True,  # Enable sentence-level timestamp
                # Add VAD configuration
                vad_kwargs={
                    # 1. Force max sentence length: Jianing subtitles usually don't exceed 10-15 characters
                    # 3000ms (3 seconds) is a good rhythm for reading
                    "max_single_segment_time": 3000,
                    # 2. End silence threshold: This is the key to "fragmented" feeling
                    # Default is usually 800ms, lowering to 500ms or lower makes the model cut at tiny pauses
                    "max_end_silence_time": 500,
                    # 3. Start silence threshold: Cooperate with end to speed up response after sentence splitting
                    "max_start_silence_time": 400,
                    # 4. Merge threshold (if parameter exists): Prevent too much fragmentation
                    # Set to a smaller value to avoid model forcing short sentences together
                    "min_pause_interval_ms": 200,
                },
            )
            is_ready = True
            print("All models loaded successfully!")
        except Exception as e:
            print(f"Model loading failed: {e}")
            raise


# --- Utility Functions ---
def time_to_srt(seconds: float) -> str:
    """Convert seconds to SRT time format: 00:00:00,000"""
    millis = int((seconds - int(seconds)) * 1000)
    secs = int(seconds % 60)
    mins = int((seconds % 3600) // 60)
    hours = int(seconds // 3600)
    return f"{hours:02d}:{mins:02d}:{secs:02d},{millis:03d}"


# --- API Endpoints ---


@app.on_event("startup")
async def startup():
    load_voice_model()


@app.get("/health")
async def health():
    return {"status": "ready" if is_ready else "loading"}


@app.post("/api/transcribe")
async def transcribe(file: UploadFile = File(...)):
    """Plain text recognition endpoint"""
    if not is_ready:
        raise HTTPException(status_code=503, detail="Model loading")

    tmp_path = f"tmp_asr_{int(time.time() * 1000)}.wav"
    try:
        content = await file.read()
        with open(tmp_path, "wb") as f:
            f.write(content)

        start_t = time.time()
        res = voice_model.generate(
            input=tmp_path, batch_size_token=5000, punc_model=None
        )

        # In Paraformer linkage mode, text is already complete text with punctuation
        full_text = res[0].get("text", "")
        # Remove punctuation
        full_text = re.sub(r"[^\w\s]", "", full_text)
        # Remove extra spaces
        full_text = re.sub(r"\s+", " ", full_text).strip()

        return {"text": full_text, "duration": time.time() - start_t, "success": True}
    finally:
        if os.path.exists(tmp_path):
            os.remove(tmp_path)


@app.post("/api/subtitle")
async def generate_subtitle(file: UploadFile = File(...), max_chars: int = Form(20)):
    """SRT subtitle generation endpoint"""
    if not is_ready:
        raise HTTPException(status_code=503, detail="Model loading")

    tmp_path = f"tmp_sub_{int(time.time() * 1000)}.wav"
    try:
        content = await file.read()
        with open(tmp_path, "wb") as f:
            f.write(content)

        start_t = time.time()
        # This generate will automatically trigger VAD splitting
        res = voice_model.generate(input=tmp_path, batch_size_token=5000)
        data = res[0]

        sentences = []
        # Case A: Model directly returned sentence_info (ideal)
        if "sentence_info" in data and data["sentence_info"]:
            raw_sentences = data["sentence_info"]
            # Further check if each sentence is too long, split by length if needed
            for s in raw_sentences:
                # Remove punctuation
                s["text"] = re.sub(r"[^\w\s]", "", s["text"]).strip()
                if s["text"]:
                    sentences.append(s)

        # Case B: Only character-level timestamps (fallback logic)
        elif "timestamp" in data and data["timestamp"]:
            chars = data["text"].replace(" ", "")
            ts = data["timestamp"]
            # Check if timestamp array is empty
            if not ts or len(ts) == 0:
                # If no timestamp, return empty subtitle
                return {
                    "srt": "",
                    "count": 0,
                    "process_time": time.time() - start_t,
                }

            curr_c, curr_start = [], ts[0][0]
            for i in range(min(len(chars), len(ts))):
                curr_c.append(chars[i])
                if chars[i] in {"。", "？", "！", "，"} or len(curr_c) >= max_chars:
                    # Remove punctuation
                    text = "".join(curr_c)
                    text = re.sub(r"[^\w\s]", "", text).strip()
                    if text:
                        sentences.append(
                            {"start": curr_start, "end": ts[i][1], "text": text}
                        )
                    if i < len(ts) - 1:
                        curr_start, curr_c = ts[i + 1][0], []

        # Generate SRT
        srt_out = []
        for i, s in enumerate(sentences, 1):
            start_str = time_to_srt(s["start"] / 1000.0)
            end_str = time_to_srt(s["end"] / 1000.0)
            srt_out.append(f"{i}\n{start_str} --> {end_str}\n{s['text']}\n")

        return {
            "srt": "\n".join(srt_out),
            "count": len(sentences),
            "process_time": time.time() - start_t,
        }
    finally:
        if os.path.exists(tmp_path):
            os.remove(tmp_path)


def find_free_port():
    """Prefer port 7860, automatically assign free port if occupied"""
    import socket

    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    try:
        sock.bind(("127.0.0.1", 7860))
        sock.close()
        return 7860
    except OSError:
        # Port 7860 is occupied, automatically assign free port
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
        sock.close()
        print(f"Port 7860 is occupied, automatically switched to port: {port}")
        return port


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(
        description="SenseVoice Paraformer Voice Recognition Service"
    )
    parser.add_argument(
        "--port",
        type=int,
        default=0,
        help="Service port number, default auto-assign free port",
    )
    args = parser.parse_args()

    print()
    print("==========================================")
    print(f"         SenseVoice  v{__version__:<17s}")
    print("      Paraformer Voice Recognition Service")
    print("==========================================")
    print()

    if args.port > 0:
        port = args.port
    else:
        port = find_free_port()

    print(f"Service address: http://127.0.0.1:{port}")

    # Fix uvicorn logging crash in PyInstaller --noconsole mode
    # Simplest solution: disable logging configuration and let uvicorn handle it
    import logging

    logging.basicConfig(level=logging.INFO)

    uvicorn.run(app, host="127.0.0.1", port=port, log_level="info")
