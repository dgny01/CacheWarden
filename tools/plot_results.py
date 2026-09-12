#!/usr/bin/env python3
"""Render GitHub-friendly SVG charts from the five-run reference benchmark."""

import csv
from pathlib import Path
from xml.sax.saxutils import escape

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / "docs" / "data" / "five-run-benchmark.csv"
OUTPUT = ROOT / "docs" / "images"
BASELINE, INTERFERENCE, TEXT, GRID = "#2563eb", "#dc2626", "#1f2937", "#d1d5db"


def averages():
    with DATA.open(newline="") as source:
        rows = list(csv.DictReader(source))
    return {key: sum(float(row[key]) for row in rows) / len(rows) for key in rows[0] if key != "run"}


def document(title, body):
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="900" height="500" viewBox="0 0 900 500" role="img" aria-labelledby="title desc">
<title id="title">{escape(title)}</title><desc id="desc">{escape(title)}</desc><rect width="900" height="500" fill="#ffffff"/>
<style>text{{font-family:Arial,sans-serif;fill:{TEXT}}}.title{{font-size:22px;font-weight:bold}}.label{{font-size:14px}}.value{{font-size:13px;font-weight:bold}}.axis{{font-size:12px;fill:#4b5563}}</style>{body}</svg>'''


def bars_chart(filename, title, labels, baseline, interference, unit, note=""):
    width, left, top, bottom = 690, 130, 80, 405
    maximum = max(baseline + interference) * 1.16
    parts = [f'<text class="title" x="{left}" y="42">{escape(title)}</text>']
    for tick in range(6):
        value, y = maximum * tick / 5, bottom - (bottom - top) * tick / 5
        parts.extend((f'<line x1="{left}" y1="{y:.1f}" x2="{left + width}" y2="{y:.1f}" stroke="{GRID}"/>', f'<text class="axis" x="{left - 12}" y="{y + 4:.1f}" text-anchor="end">{value:.0f}</text>'))
    group_width, bar_width = width / len(labels), min(70, width / len(labels) * 0.28)
    for index, label in enumerate(labels):
        center = left + group_width * (index + 0.5)
        for offset, value, color in ((-bar_width - 4, baseline[index], BASELINE), (4, interference[index], INTERFERENCE)):
            height, x, y = value / maximum * (bottom - top), center + offset, bottom - value / maximum * (bottom - top)
            parts.extend((f'<rect x="{x:.1f}" y="{y:.1f}" width="{bar_width:.1f}" height="{height:.1f}" rx="2" fill="{color}"/>', f'<text class="value" x="{x + bar_width / 2:.1f}" y="{y - 8:.1f}" text-anchor="middle">{value:.2f}{unit}</text>'))
        parts.append(f'<text class="label" x="{center}" y="435" text-anchor="middle">{escape(label)}</text>')
    parts.extend((f'<rect x="{left}" y="462" width="14" height="14" fill="{BASELINE}"/><text class="label" x="{left + 21}" y="474">Baseline</text>', f'<rect x="{left + 125}" y="462" width="14" height="14" fill="{INTERFERENCE}"/><text class="label" x="{left + 146}" y="474">Interference</text>'))
    if note:
        parts.append(f'<text class="label" x="{left + width}" y="474" text-anchor="end">{escape(note)}</text>')
    (OUTPUT / filename).write_text(document(title, "\n".join(parts)))


def main():
    OUTPUT.mkdir(parents=True, exist_ok=True)
    data = averages()
    metrics = ("mean", "p50", "p95", "p99")
    bars_chart("latency-comparison.svg", "Five-run average victim latency", ["Mean", "p50", "p95", "p99"], [data[f"baseline_{metric}_ms"] for metric in metrics], [data[f"interference_{metric}_ms"] for metric in metrics], " ms")
    baseline, interference = data["baseline_requests"], data["interference_requests"]
    bars_chart("completed-requests.svg", "Five-run average completed requests", ["Completed requests"], [baseline], [interference], "", f"{(interference / baseline - 1) * 100:.2f}% with aggressor active")


if __name__ == "__main__":
    main()
