"""Рисует иконки POSvpn: щит с буквой P.

Клиент — зелёный, сервер — красный. Скрипт держим в репозитории, чтобы иконки
можно было пересобрать: python tools/make-icons.py
"""

from PIL import Image, ImageDraw, ImageFont
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
ASSETS = ROOT / "assets"
ASSETS.mkdir(exist_ok=True)

SIZES = [16, 24, 32, 48, 64, 128, 256]


def bezier(p0, p1, p2, steps=24):
    points = []
    for i in range(steps + 1):
        t = i / steps
        x = (1 - t) ** 2 * p0[0] + 2 * (1 - t) * t * p1[0] + t ** 2 * p2[0]
        y = (1 - t) ** 2 * p0[1] + 2 * (1 - t) * t * p1[1] + t ** 2 * p2[1]
        points.append((x, y))
    return points


def shield_points(size):
    left, right = 0.10 * size, 0.90 * size
    top, shoulder, tip = 0.12 * size, 0.50 * size, 0.94 * size
    mid = 0.5 * size
    points = [(left, top), (right, top), (right, shoulder)]
    points += bezier((right, shoulder), (right, 0.80 * size), (mid, tip))
    points += bezier((mid, tip), (left, 0.80 * size), (left, shoulder))[::-1]
    return points


def font(size):
    for name in ("arialbd.ttf", "segoeuib.ttf", "DejaVuSans-Bold.ttf"):
        try:
            return ImageFont.truetype(name, size)
        except OSError:
            continue
    return ImageFont.load_default()


def make(size, base, dark, light, letter, path):
    scale = 4
    canvas = size * scale
    img = Image.new("RGBA", (canvas, canvas), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)

    points = shield_points(canvas)

    # Плавный градиент сверху вниз, обрезанный по форме щита.
    gradient = Image.new("RGBA", (canvas, canvas))
    gdraw = ImageDraw.Draw(gradient)
    for y in range(canvas):
        t = y / max(1, canvas - 1)
        color = tuple(int(light[i] + (base[i] - light[i]) * t) for i in range(3))
        gdraw.line([(0, y), (canvas, y)], fill=color + (255,))
    mask = Image.new("L", (canvas, canvas), 0)
    ImageDraw.Draw(mask).polygon(points, fill=255)
    img.paste(gradient, (0, 0), mask)

    draw.line(points + [points[0]], fill=dark + (255,), width=int(canvas * 0.03), joint="curve")

    f = font(int(canvas * 0.54))
    box = draw.textbbox((0, 0), letter, font=f)
    tx = canvas / 2 - (box[2] - box[0]) / 2 - box[0]
    ty = canvas * 0.45 - (box[3] - box[1]) / 2 - box[1]
    draw.text((tx + canvas * 0.008, ty + canvas * 0.008), letter, font=f, fill=(0, 0, 0, 90))
    draw.text((tx, ty), letter, font=f, fill=(255, 255, 255, 255))

    img = img.resize((size, size), Image.LANCZOS)
    img.save(path, format="ICO", sizes=[(s, s) for s in SIZES if s <= size])


def main():
    make(256, (0x43, 0xC0, 0x7B), (0x24, 0x7A, 0x4B), (0x7B, 0xE3, 0xA9), "P", ASSETS / "icon-client.ico")
    make(256, (0xE2, 0x57, 0x4C), (0x8F, 0x2F, 0x28), (0xFF, 0x8B, 0x80), "P", ASSETS / "icon-server.ico")
    print("готово:", ASSETS / "icon-client.ico", ASSETS / "icon-server.ico")


if __name__ == "__main__":
    main()
