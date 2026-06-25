#!/usr/bin/env bash
# Downloads the pinned browser libraries that get embedded into the binary.
# Run once before `go build`. Commit the downloaded files.
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

curl -fsSL https://cdn.jsdelivr.net/npm/chart.js@4.4.1/dist/chart.umd.js          -o "$DIR/chart.umd.js"
curl -fsSL https://cdn.jsdelivr.net/npm/html2canvas@1.4.1/dist/html2canvas.min.js -o "$DIR/html2canvas.min.js"
curl -fsSL https://cdn.jsdelivr.net/npm/jspdf@2.5.1/dist/jspdf.umd.min.js         -o "$DIR/jspdf.umd.min.js"

echo "Vendored Chart.js 4.4.1, html2canvas 1.4.1, jsPDF 2.5.1 into $DIR"
