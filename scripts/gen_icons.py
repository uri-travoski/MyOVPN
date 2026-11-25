#!/usr/bin/env python3
"""Generate MyOVPN tray and launcher icons (PNG, ICO, SVG, Android XML) from the OpenVPN SVG path."""

import io
import os
import subprocess
from pathlib import Path
from PIL import Image

ROOT = Path(__file__).resolve().parent.parent
ASSETS_DIR = ROOT / "assets" / "icons"
EMBED_DIR = ROOT / "internal" / "assets" / "icons"
ANDROID_RES = ROOT / "android" / "app" / "src" / "main" / "res"

OVPN_PATH = (
    "M12 .357C5.385.357 0 5.69 0 12.254c0 4.36 2.358 8.153 5.896 10.204l.77-5.076"
    "a7.046 7.046 0 01-1.846-4.719c0-3.897 3.18-7.076 7.13-7.076 3.948 0 7.126 3.18 "
    "7.126 7.076 0 1.847-.717 3.488-1.846 4.77L18 22.51c3.59-2.05 6-5.899 6-10.258"
    "C24 5.69 18.615.357 12 .357zm-.05 8.157a3.786 3.786 0 00-3.796 3.795 "
    "3.738 3.738 0 002.461 3.54L9.13 23.643h5.64l-1.435-7.795c1.385-.564 "
    "2.41-1.898 2.41-3.54a3.786 3.786 0 00-3.795-3.794z"
)

VARIANTS = {
    "white": ("#FFFFFF", "#1E293B", "1.1"),
    "green": ("#22C55E", "#0F172A", "0.9"),
    "red": ("#EF4444", "#0F172A", "0.9"),
    "brand": ("#EA7E20", "#1E293B", "0.6"),
}

LINUX_SIZES = [22, 24, 32, 48, 128, 256]
ICO_SIZES = [16, 24, 32, 48, 64, 128, 256]


def make_svg(fill: str, stroke: str, stroke_width: str, padded: bool = True) -> str:
    # Pad viewBox slightly so thin dark outline is never clipped at edges
    vb = "-1.2 -1.2 26.4 26.4" if padded else "0 0 24 24"
    return (
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{vb}" width="256" height="256">\n'
        f'  <path d="{OVPN_PATH}" fill="{fill}" stroke="{stroke}" '
        f'stroke-width="{stroke_width}" stroke-linejoin="round" stroke-linecap="round"/>\n'
        f'</svg>\n'
    )


def render_svg_to_png(svg_path: Path, png_path: Path, size: int) -> None:
    cmd = ["rsvg-convert", "-w", str(size), "-h", str(size), str(svg_path), "-o", str(png_path)]
    try:
        subprocess.run(cmd, check=True)
    except FileNotFoundError:
        subprocess.run(["distrobox", "enter", "devbox", "--", *cmd], check=True)


def build_ico(png_paths: dict[int, Path], ico_path: Path) -> None:
    imgs = [Image.open(png_paths[s]).convert("RGBA") for s in ICO_SIZES]
    imgs[-1].save(
        ico_path,
        format="ICO",
        sizes=[(s, s) for s in ICO_SIZES],
        append_images=imgs[:-1],
    )


def write_android_vector(path: Path, fill: str, stroke: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    xml = f"""<vector xmlns:android="http://schemas.android.com/apk/res/android"
    android:width="24dp"
    android:height="24dp"
    android:viewportWidth="26.4"
    android:viewportHeight="26.4">
    <group android:translateX="1.2" android:translateY="1.2">
        <path
            android:fillColor="{fill}"
            android:strokeColor="{stroke}"
            android:strokeWidth="0.9"
            android:strokeLineJoin="round"
            android:pathData="{OVPN_PATH}" />
    </group>
</vector>
"""
    path.write_text(xml, encoding="utf-8")


def main() -> None:
    ASSETS_DIR.mkdir(parents=True, exist_ok=True)
    EMBED_DIR.mkdir(parents=True, exist_ok=True)

    for name, (fill, stroke, sw) in VARIANTS.items():
        svg_file = ASSETS_DIR / f"myovpn-{name}.svg"
        svg_file.write_text(make_svg(fill, stroke, sw), encoding="utf-8")

        png_by_size: dict[int, Path] = {}
        all_sizes = sorted(set(LINUX_SIZES + ICO_SIZES))
        for size in all_sizes:
            out_png = ASSETS_DIR / f"myovpn-{name}-{size}.png"
            render_svg_to_png(svg_file, out_png, size)
            png_by_size[size] = out_png

        ico_out = ASSETS_DIR / f"myovpn-{name}.ico"
        build_ico(png_by_size, ico_out)

        # Embed 32px PNG (and 64px for HiDPI) + ICO for tray runtime
        if name in ("white", "green", "red"):
            (EMBED_DIR / f"tray-{name}.png").write_bytes((ASSETS_DIR / f"myovpn-{name}-32.png").read_bytes())
            (EMBED_DIR / f"tray-{name}.ico").write_bytes(ico_out.read_bytes())

    # Canonical app icons
    (ASSETS_DIR / "myovpn.svg").write_text(make_svg("#EA7E20", "#1E293B", "0.6", padded=False), encoding="utf-8")
    (ASSETS_DIR / "myovpn.ico").write_bytes((ASSETS_DIR / "myovpn-brand.ico").read_bytes())
    (ASSETS_DIR / "myovpn.png").write_bytes((ASSETS_DIR / "myovpn-brand-256.png").read_bytes())

    # Android vector drawables
    write_android_vector(ANDROID_RES / "drawable" / "ic_vpn_white.xml", "#FFFFFF", "#1E293B")
    write_android_vector(ANDROID_RES / "drawable" / "ic_vpn_green.xml", "#22C55E", "#0F172A")
    write_android_vector(ANDROID_RES / "drawable" / "ic_vpn_red.xml", "#EF4444", "#0F172A")
    write_android_vector(ANDROID_RES / "drawable" / "ic_launcher_foreground.xml", "#FFFFFF", "#1E293B")
    print("Generated all MyOVPN icons successfully.")


if __name__ == "__main__":
    main()
