#!/usr/bin/env python3
"""Generate local Chinese narration and matching Remotion timing/caption assets."""

from __future__ import annotations

import json
import math
import os
import re
import shutil
import subprocess
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
FPS = 30
TRANSITION_FRAMES = 18
HEAD_PAD_MS = 260
TAIL_PAD_SECONDS = 0.85
VOICE = os.environ.get("MR_TTS_VOICE", "Tingting")
RATE = os.environ.get("MR_TTS_RATE", "180")
STORY_PATH = ROOT / "src/scenes/story.json"
AUDIO_DIR = ROOT / "public/audio"


def probe_duration(path: Path) -> float:
    result = subprocess.run(
        [
            "ffprobe", "-v", "error", "-show_entries", "format=duration",
            "-of", "default=nokey=1:noprint_wrappers=1", str(path),
        ],
        check=True,
        capture_output=True,
        text=True,
    )
    return float(result.stdout.strip())


def srt_time(ms: int) -> str:
    hours, rem = divmod(ms, 3_600_000)
    minutes, rem = divmod(rem, 60_000)
    seconds, milliseconds = divmod(rem, 1_000)
    return f"{hours:02}:{minutes:02}:{seconds:02},{milliseconds:03}"


def wrap_subtitle(text: str, width: int = 30) -> str:
    if len(text) <= width:
        return text
    middle = len(text) // 2
    boundaries = [i + 1 for i, character in enumerate(text) if character in "，。；：、！？"]
    candidates = [point for point in boundaries if 14 <= point <= len(text) - 14]
    split_at = min(candidates, key=lambda point: abs(point - middle)) if candidates else width
    if not candidates:
        spaces = [match.end() for match in re.finditer(r"\s+", text) if 14 <= match.end() <= len(text) - 14]
        if spaces:
            split_at = min(spaces, key=lambda point: abs(point - middle))
    return text[:split_at] + "\n" + text[split_at:]


def run() -> None:
    for binary in ("say", "ffmpeg", "ffprobe"):
        if shutil.which(binary) is None:
            raise SystemExit(f"Required command not found: {binary}")

    scenes = json.loads(STORY_PATH.read_text(encoding="utf-8"))
    AUDIO_DIR.mkdir(parents=True, exist_ok=True)
    timing_scenes = []
    captions = []
    cursor = 0

    with tempfile.TemporaryDirectory(prefix="mr-voiceover-") as temp_dir:
        temp = Path(temp_dir)
        for scene in scenes:
            scene_id = scene["id"]
            source = temp / f"{scene_id}.aiff"
            output = AUDIO_DIR / f"{scene_id}.mp3"

            subprocess.run(
                ["say", "-v", VOICE, "-r", RATE, "-o", str(source), scene["narration"]],
                check=True,
            )
            narration_duration = probe_duration(source)
            subprocess.run(
                [
                    "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
                    "-i", str(source), "-af",
                    f"adelay={HEAD_PAD_MS}|{HEAD_PAD_MS},apad=pad_dur={TAIL_PAD_SECONDS},loudnorm=I=-16:TP=-1.5:LRA=11",
                    "-codec:a", "libmp3lame", "-b:a", "160k", str(output),
                ],
                check=True,
            )
            clip_duration = probe_duration(output)
            duration_frames = math.ceil(clip_duration * FPS) + 1
            timing_scenes.append({
                "id": scene_id,
                "startFrame": cursor,
                "durationInFrames": duration_frames,
                "narrationDurationMs": round(narration_duration * 1000),
                "audioDurationMs": round(clip_duration * 1000),
            })
            captions.append({
                "text": scene["narration"],
                "startMs": round(cursor * 1000 / FPS) + HEAD_PAD_MS,
                "endMs": round(cursor * 1000 / FPS) + HEAD_PAD_MS + round(narration_duration * 1000),
                "timestampMs": None,
                "confidence": None,
            })
            cursor += duration_frames - TRANSITION_FRAMES

    duration_frames = cursor + TRANSITION_FRAMES
    timing = {
        "fps": FPS,
        "transitionFrames": TRANSITION_FRAMES,
        "durationInFrames": duration_frames,
        "durationSeconds": round(duration_frames / FPS, 2),
        "scenes": timing_scenes,
    }
    (ROOT / "src/scenes/generated-timing.json").write_text(
        json.dumps(timing, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    (ROOT / "public/captions.json").write_text(
        json.dumps(captions, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    srt = []
    for index, caption in enumerate(captions, start=1):
        srt.append(
            f"{index}\n{srt_time(caption['startMs'])} --> {srt_time(caption['endMs'])}\n"
            f"{wrap_subtitle(caption['text'])}"
        )
    (ROOT / "public/zh-CN.srt").write_text("\n\n".join(srt) + "\n", encoding="utf-8")
    print(f"Generated {len(scenes)} narration clips · {duration_frames / FPS:.1f}s · voice={VOICE} rate={RATE}")
    for scene in timing_scenes:
        print(f"{scene['id']}: {scene['narrationDurationMs'] / 1000:.1f}s speech")


if __name__ == "__main__":
    run()
