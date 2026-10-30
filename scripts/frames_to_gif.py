"""Assembles PNG frames into an animated GIF.

Usage: frames_to_gif.py <frames-dir> <out.gif> <frame-ms> <max-width>
"""

import pathlib
import sys

from PIL import Image


def main() -> None:
    frames_dir, out, frame_ms, max_width = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
    paths = sorted(pathlib.Path(frames_dir).glob("frame-*.png"))
    if not paths:
        raise SystemExit("no frames found")
    images = []
    for path in paths:
        img = Image.open(path).convert("RGB")
        if img.width > max_width:
            ratio = max_width / img.width
            img = img.resize((max_width, round(img.height * ratio)), Image.Resampling.LANCZOS)
        images.append(img.convert("P", palette=Image.Palette.ADAPTIVE, colors=128))
    images[0].save(
        out,
        save_all=True,
        append_images=images[1:],
        duration=frame_ms,
        loop=0,
        optimize=True,
    )
    print(f"wrote {out} from {len(images)} frames")


if __name__ == "__main__":
    main()
