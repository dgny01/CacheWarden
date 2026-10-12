#!/usr/bin/env python3
"""Render GitHub-friendly SVG charts from committed reference results."""

import csv
from pathlib import Path
from xml.sax.saxutils import escape

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / "docs" / "data" / "five-run-benchmark.csv"
SCHEDULER_DATA = ROOT / "docs" / "data" / "four-run-scheduler.csv"
OUTPUT = ROOT / "docs" / "images"
BASELINE, INTERFERENCE, TEXT, GRID = "#2563eb", "#dc2626", "#1f2937", "#d1d5db"


def averages():
    with DATA.open(newline="") as source:
        rows = list(csv.DictReader(source))
    return {key: sum(float(row[key]) for row in rows) / len(rows) for key in rows[0] if key != "run"}


def average_pair_change(baseline_key, interference_key):
    with DATA.open(newline="") as source:
        rows = list(csv.DictReader(source))
    return sum(
        (float(row[interference_key]) / float(row[baseline_key]) - 1) * 100
        for row in rows
    ) / len(rows)


def document(title, body, height=500):
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="900" height="{height}" viewBox="0 0 900 {height}" role="img" aria-labelledby="title desc">
<title id="title">{escape(title)}</title><desc id="desc">{escape(title)}</desc><rect width="900" height="{height}" fill="#ffffff"/>
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


def scheduler_chart():
    with SCHEDULER_DATA.open(newline="") as source:
        rows = list(csv.DictReader(source))
    if not rows:
        raise ValueError(f"no scheduler results found in {SCHEDULER_DATA}")

    baseline = [int(row["baseline_mean_delay_ns"]) / 1_000_000 for row in rows]
    interference = [int(row["interference_mean_delay_ns"]) / 1_000_000 for row in rows]
    baseline_mean = sum(baseline) / len(baseline)
    interference_mean = sum(interference) / len(interference)
    change = (interference_mean / baseline_mean - 1) * 100

    width, left, top, bottom = 690, 130, 90, 405
    maximum = max(baseline + interference) * 1.16
    parts = [
        f'<text class="title" x="{left}" y="42">Mean scheduler run-queue delay by experiment</text>',
        f'<rect x="{left}" y="54" width="14" height="14" fill="{BASELINE}"/><text class="label" x="{left + 21}" y="66">Baseline</text>',
        f'<rect x="{left + 125}" y="54" width="14" height="14" fill="{INTERFERENCE}"/><text class="label" x="{left + 146}" y="66">Interference</text>',
        f'<text class="label" x="{left + width}" y="65" text-anchor="end">+{change:.2f}% between unweighted condition means</text>',
        f'<text class="label" x="24" y="{(top + bottom) / 2:.1f}" text-anchor="middle" transform="rotate(-90 24 {(top + bottom) / 2:.1f})">Mean scheduler delay (ms)</text>',
    ]
    for tick in range(6):
        value, y = maximum * tick / 5, bottom - (bottom - top) * tick / 5
        parts.extend((
            f'<line x1="{left}" y1="{y:.1f}" x2="{left + width}" y2="{y:.1f}" stroke="{GRID}"/>',
            f'<text class="axis" x="{left - 12}" y="{y + 4:.1f}" text-anchor="end">{value:.1f}</text>',
        ))

    labels = [f'{row["run_id"][4:6]}/{row["run_id"][6:8]} {row["run_id"][9:11]}:{row["run_id"][11:13]}' for row in rows]
    group_width, bar_width = width / len(rows), 48
    for index, label in enumerate(labels):
        center = left + group_width * (index + 0.5)
        for offset, value, color in ((-bar_width - 4, baseline[index], BASELINE), (4, interference[index], INTERFERENCE)):
            height = value / maximum * (bottom - top)
            x, y = center + offset, bottom - height
            parts.extend((
                f'<rect x="{x:.1f}" y="{y:.1f}" width="{bar_width}" height="{height:.1f}" rx="2" fill="{color}"/>',
                f'<text class="value" x="{x + bar_width / 2:.1f}" y="{y - 8:.1f}" text-anchor="middle">{value:.2f}</text>',
            ))
        parts.append(f'<text class="axis" x="{center:.1f}" y="430" text-anchor="middle">{label}</text>')

    parts.extend((
        f'<text class="label" x="{left + width / 2}" y="453" text-anchor="middle">Experiment start (UTC)</text>',
    ))
    (OUTPUT / "scheduler-delay-comparison.svg").write_text(
        document("Mean scheduler run-queue delay by experiment", "\n".join(parts), height=540)
    )


def main():
    OUTPUT.mkdir(parents=True, exist_ok=True)
    data = averages()
    metrics = ("mean", "p50", "p95", "p99")
    bars_chart("latency-comparison.svg", "Five-run average victim latency", ["Mean", "p50", "p95", "p99"], [data[f"baseline_{metric}_ms"] for metric in metrics], [data[f"interference_{metric}_ms"] for metric in metrics], " ms")
    baseline, interference = data["baseline_requests"], data["interference_requests"]
    request_change = average_pair_change("baseline_requests", "interference_requests")
    bars_chart("completed-requests.svg", "Five-run average completed requests", ["Completed requests"], [baseline], [interference], "", f"Mean within-pair change: {request_change:.2f}%")
    scheduler_chart()


if __name__ == "__main__":
    main()
