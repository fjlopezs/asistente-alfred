"""Write a shields-style SVG badge for a coverage percentage.

Usage: coverage_badge.py <percent> <out.svg>
"""
import sys

pct = float(sys.argv[1])
color = "#4c1" if pct >= 80 else "#a4a61d" if pct >= 60 else "#dfb317" if pct >= 40 else "#e05d44"
label, value = "coverage", f"{pct:.1f}%"
# Verdana 11px averages ~7px per character; good enough for a static badge.
lw, vw = 6 * len(label) + 12, 7 * len(value) + 10
w = lw + vw

svg = f"""<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="20" role="img" aria-label="{label}: {value}">
<title>{label}: {value}</title>
<linearGradient id="s" x2="0" y2="100%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient>
<clipPath id="r"><rect width="{w}" height="20" rx="3" fill="#fff"/></clipPath>
<g clip-path="url(#r)"><rect width="{lw}" height="20" fill="#555"/><rect x="{lw}" width="{vw}" height="20" fill="{color}"/><rect width="{w}" height="20" fill="url(#s)"/></g>
<g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="11">
<text x="{lw / 2}" y="15" fill="#010101" fill-opacity=".3">{label}</text><text x="{lw / 2}" y="14">{label}</text>
<text x="{lw + vw / 2}" y="15" fill="#010101" fill-opacity=".3">{value}</text><text x="{lw + vw / 2}" y="14">{value}</text>
</g></svg>
"""
with open(sys.argv[2], "w") as f:
    f.write(svg)
