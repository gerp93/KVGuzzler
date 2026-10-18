#!/usr/bin/env python3
"""Generate every icon size/format from the one source mark, assets/logo.png.

Never hand-export icons: they all come from this script (KVG_Standards' logo & branding
checklist; KVGrainy's scripts/generate_icons.py is the reference). Outputs, all committed:
  assets/icon.png  256x256   Window.Icon, the Linux AppImage (release workflow `icon_png`)
  assets/icon.ico  multi-size  <ApplicationIcon> and the Windows installer (`icon_ico`)

Needs Pillow:  pip install pillow
Run from the repo root:  python scripts/generate-icons.py
"""
import sys
from pathlib import Path

from PIL import Image

ROOT = Path(__file__).resolve().parent.parent
LOGO = ROOT / "assets" / "logo.png"
ICO_SIZES = [(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)]


def square(img: Image.Image) -> Image.Image:
    """Pad to a square on a transparent canvas, never stretching the mark."""
    side = max(img.size)
    canvas = Image.new("RGBA", (side, side), (0, 0, 0, 0))
    canvas.paste(img, ((side - img.width) // 2, (side - img.height) // 2))
    return canvas


def main() -> int:
    if not LOGO.exists():
        print(f"{LOGO.relative_to(ROOT)} does not exist yet. Add the source mark (a high-resolution "
              "PNG), then run this again.", file=sys.stderr)
        return 1
    mark = square(Image.open(LOGO).convert("RGBA"))
    mark.resize((256, 256), Image.LANCZOS).save(ROOT / "assets" / "icon.png")
    mark.resize((256, 256), Image.LANCZOS).save(ROOT / "assets" / "icon.ico", sizes=ICO_SIZES)
    print("Wrote assets/icon.png and assets/icon.ico")
    return 0


if __name__ == "__main__":
    sys.exit(main())
