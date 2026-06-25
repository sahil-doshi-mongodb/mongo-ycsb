const MAX = 5;
const palette = ['#00ED64', '#016BF8', '#FF6960', '#FFC010', '#B45AF2'];

const state = {
    runs: [],          // list items from /api/runs
    selected: [],      // ordered run_ids (max 5)
    details: {},       // run_id -> full RunResult
    sortKey: 'timestamp',
    sortDir: -1,
    search: '',
    workload: '',
    charts: [],
};

document.addEventListener('DOMContentLoaded', init);

async function init() {
    wireControls();
    await loadRuns();
}

async function loadRuns() {
    try {
        const res = await fetch('/api/runs');
        if (!res.ok) throw new Error(res.status);
        state.runs = await res.json();
    } catch (e) {
        toast('Failed to load runs');
        return;
    }
    populateWorkloadFilter();
    renderTable();
    document.getElementById('conn').textContent = `${state.runs.length} runs`;
}

function wireControls() {
    document.getElementById('search').addEventListener('input', e => { state.search = e.target.value; renderTable(); });
    document.getElementById('workload-filter').addEventListener('change', e => { state.workload = e.target.value; renderTable(); });
    document.getElementById('clear-sel').addEventListener('click', () => {
        state.selected = []; updateSelCount(); renderTable(); renderCompare();
    });
    document.getElementById('export-pdf').addEventListener('click', exportPDF);
    document.getElementById('export-excel').addEventListener('click', exportExcel);
    document.querySelectorAll('#runs-table thead th[data-sort]').forEach(th => {
        th.addEventListener('click', () => {
            const k = th.dataset.sort;
            if (state.sortKey === k) state.sortDir *= -1;
            else { state.sortKey = k; state.sortDir = (k === 'timestamp') ? -1 : 1; }
            renderTable();
        });
    });
}

function populateWorkloadFilter() {
    const sel = document.getElementById('workload-filter');
    const set = [...new Set(state.runs.map(r => r.workload).filter(Boolean))].sort();
    for (const w of set) {
        const o = document.createElement('option');
        o.value = w; o.textContent = w;
        sel.appendChild(o);
    }
}

function filteredSortedRuns() {
    let rows = state.runs.slice();
    const q = state.search.trim().toLowerCase();
    if (q) {
        rows = rows.filter(r =>
            (r.run_id || '').toLowerCase().includes(q) ||
            (r.workload || '').toLowerCase().includes(q) ||
            (r.tags || []).join(' ').toLowerCase().includes(q));
    }
    if (state.workload) rows = rows.filter(r => r.workload === state.workload);
    const k = state.sortKey, d = state.sortDir;
    rows.sort((a, b) => {
        let va = a[k], vb = b[k];
        if (k === 'timestamp') { va = new Date(va).getTime(); vb = new Date(vb).getTime(); }
        if (va == null) va = ''; if (vb == null) vb = '';
        if (typeof va === 'string') return d * va.localeCompare(vb);
        return d * (va - vb);
    });
    return rows;
}

function renderTable() {
    const body = document.getElementById('runs-body');
    const rows = filteredSortedRuns();
    const atMax = state.selected.length >= MAX;
    body.innerHTML = rows.map(r => {
        const sel = state.selected.includes(r.run_id);
        const idx = state.selected.indexOf(r.run_id);
        const dot = sel ? `<span class="dot" style="background:${palette[idx % palette.length]}"></span>` : '';
        return `<tr class="${sel ? 'sel' : ''}">
      <td><input type="checkbox" data-id="${esc(r.run_id)}" ${sel ? 'checked' : ''} ${(!sel && atMax) ? 'disabled' : ''}></td>
      <td>${fmtTime(r.timestamp)}</td>
      <td class="mono">${dot}${shortId(r.run_id)}</td>
      <td>${esc(r.workload || '—')}</td>
      <td>${r.threads ?? '—'}</td>
      <td>${fmtOps(r.ops_per_sec)}</td>
      <td>${fmtInt(r.total_ops)}</td>
      <td>${fmtInt(r.total_errors)}</td>
      <td>${esc(r.mongo_version || '—')}</td>
      <td>${esc((r.tags || []).join(', ') || '—')}</td>
    </tr>`;
    }).join('');
    body.querySelectorAll('input[type=checkbox]').forEach(cb => {
        cb.addEventListener('change', e => toggleSelect(e.target.dataset.id, e.target.checked));
    });
}

async function toggleSelect(id, checked) {
    if (checked) {
        if (state.selected.length >= MAX) return;
        if (!state.selected.includes(id)) state.selected.push(id);
        if (!state.details[id]) {
            try {
                const res = await fetch(`/api/runs/${encodeURIComponent(id)}`);
                if (res.ok) state.details[id] = await res.json();
                else toast('Failed to load run detail');
            } catch (e) { toast('Failed to load run detail'); }
        }
    } else {
        state.selected = state.selected.filter(x => x !== id);
    }
    updateSelCount();
    renderTable();
    renderCompare();
}

function updateSelCount() {
    document.getElementById('selcount').textContent = `${state.selected.length} / ${MAX} selected`;
}

// ── comparison rendering ────────────────────────────────────────────────────

function renderCompare() {
    destroyCharts();
    const panel = document.getElementById('compare-panel');
    const content = document.getElementById('compare-content');
    const runs = state.selected.map(id => state.details[id]).filter(Boolean);
    if (runs.length === 0) { panel.hidden = true; content.innerHTML = ''; return; }
    panel.hidden = false;
    content.innerHTML =
        metaSection(runs) + summarySection(runs) + latencySection(runs) +
        opcounterSection(runs) + errorSection(runs) + chartsSection();
    buildCharts(runs);
}

function metaSection(runs) {
    const rows = [
        ['Run ID', r => r.run_id],
        ['Timestamp (UTC)', r => fmtTime(r.timestamp)],
        ['Benchmark Start', r => fmtTime(r.benchmark_start_time)],
        ['Benchmark End', r => fmtTime(r.benchmark_end_time)],
        ['Run Start', r => fmtTime(r.run_start_time)],
        ['Run End', r => fmtTime(r.run_end_time)],
        ['Workload', r => r.config?.workload ?? '—'],
        ['Mode', r => r.config?.mode ?? '—'],
        ['Threads', r => r.config?.threads ?? '—'],
        ['Duration', r => r.config?.duration || '—'],
        ['Key Distribution', r => r.config?.key_distribution ?? '—'],
        ['Record Count', r => r.config?.record_count ?? '—'],
        ['Database', r => r.config?.database ?? '—'],
        ['Collection', r => r.config?.collection ?? '—'],
        ['Tags', r => (r.tags || []).join(', ') || '—'],
        ['MongoDB Version', r => r.cluster_info?.mongo_version ?? 'N/A'],
        ['Host', r => r.cluster_info?.host ?? 'N/A'],
        ['Storage Engine', r => r.cluster_info?.storage_engine ?? 'N/A'],
    ];
    const body = rows.map(([label, fn]) =>
        `<tr><td class="rowlabel">${label}</td>${runs.map(r => `<td>${esc(fn(r))}</td>`).join('')}</tr>`).join('');
    return section('Run Metadata', `<table class="cmp">${headRow(runs)}<tbody>${body}</tbody></table>`);
}

function summarySection(runs) {
    let rows = '';
    rows += metricRow('Throughput (ops/s)', runs.map(r => r.summary?.ops_per_sec), false, fmtOps);
    rows += metricRow('Total Ops', runs.map(r => r.summary?.total_ops), null, fmtInt);
    rows += metricRow('Total Errors', runs.map(r => r.summary?.total_errors), true, fmtInt);
    rows += metricRow('Duration (s)', runs.map(r => r.summary?.duration_seconds), null, v => fmtNum(v, 2));
    return section('Summary', `<table class="cmp">${headRow(runs)}<tbody>${rows}</tbody></table>`);
}

function latencySection(runs) {
    let out = '';
    for (const op of unionOps(runs)) {
        const m = runs.map(r => r.summary?.by_operation?.[op]);
        let rows = '';
        rows += metricRow('Count', m.map(x => x?.count), null, fmtInt);
        rows += metricRow('Errors', m.map(x => x?.errors), true, fmtInt);
        rows += metricRow('Mean (ms)', m.map(x => x?.mean_ms), true, v => fmtNum(v, 2));
        rows += metricRow('p50 (ms)', m.map(x => x?.p50_ms), true, v => fmtNum(v, 2));
        rows += metricRow('p95 (ms)', m.map(x => x?.p95_ms), true, v => fmtNum(v, 2));
        rows += metricRow('p99 (ms)', m.map(x => x?.p99_ms), true, v => fmtNum(v, 2));
        rows += metricRow('p99.9 (ms)', m.map(x => x?.p999_ms), true, v => fmtNum(v, 2));
        rows += metricRow('p99.99 (ms)', m.map(x => x?.p9999_ms), true, v => fmtNum(v, 2));
        rows += metricRow('p99.999 (ms)', m.map(x => x?.p99999_ms), true, v => fmtNum(v, 2));
        out += `<h4 class="op">${esc(op)}</h4><table class="cmp">${headRow(runs)}<tbody>${rows}</tbody></table>`;
    }
    return section('Latency by Operation', out || '<p class="muted">No operation metrics.</p>');
}

function opcounterSection(runs) {
    const fields = [['Insert', 'insert'], ['Query', 'query'], ['Update', 'update'],
    ['Delete', 'delete'], ['GetMore', 'getmore'], ['Command', 'command']];
    const rows = fields.map(([label, key]) =>
        metricRow(label, runs.map(r => r.server_stats?.delta?.[key]), null, fmtInt)).join('');
    return section('Server Opcounter Deltas', `<table class="cmp">${headRow(runs)}<tbody>${rows}</tbody></table>`);
}

function errorSection(runs) {
    const any = runs.some(r => (r.error_samples || []).length);
    if (!any) return section('Error Samples', '<p class="muted">No errors recorded.</p>');
    const cols = runs.map((r, i) => {
        const items = (r.error_samples || []).slice(0, 10)
            .map(e => `<li><code>${esc(e.operation)}</code> ${esc(e.message)}</li>`).join('');
        return `<div class="errcol"><h5><span class="dot" style="background:${palette[i % palette.length]}"></span>${shortId(r.run_id)}</h5><ul>${items || '<li class="muted">none</li>'}</ul></div>`;
    }).join('');
    return section('Error Samples', `<div class="errgrid">${cols}</div>`);
}

function chartsSection() {
    return section('Time Series', `
    <div class="charts">
      <div class="chartbox"><canvas id="chart-throughput"></canvas></div>
      <div class="chartbox"><canvas id="chart-p99"></canvas></div>
      <div class="chartbox"><canvas id="chart-cpu"></canvas></div>
      <div class="chartbox"><canvas id="chart-mem"></canvas></div>
    </div>`);
}

function buildCharts(runs) {
    buildChart('chart-throughput', 'Throughput (ops/sec)', runs,
        r => (r.delta || []).map(d => ({ x: d.offset_seconds, y: d.ops_per_sec })), 'ops/sec');
    buildChart('chart-p99', 'p99 Latency (ms)', runs,
        r => (r.delta || []).map(d => ({ x: d.offset_seconds, y: d.p99_ms })), 'ms');
    buildChart('chart-cpu', 'CPU (%)', runs,
        r => (r.system_samples || []).map(s => ({ x: s.offset_seconds, y: s.cpu_percent })), '%');
    buildChart('chart-mem', 'Memory (MB)', runs,
        r => (r.system_samples || []).map(s => ({ x: s.offset_seconds, y: s.memory_mb })), 'MB');
}

function buildChart(canvasId, title, runs, pick, yLabel) {
    const el = document.getElementById(canvasId);
    if (!el) return;
    const datasets = runs.map((rr, i) => ({
        label: shortId(rr.run_id),
        data: pick(rr),
        borderColor: palette[i % palette.length],
        backgroundColor: palette[i % palette.length],
        borderWidth: 2, pointRadius: 0, tension: 0.2,
    }));
    const chart = new Chart(el, {
        type: 'line',
        data: { datasets },
        options: {
            responsive: true, maintainAspectRatio: false, animation: false,
            plugins: { title: { display: true, text: title }, legend: { position: 'bottom' } },
            scales: {
                x: { type: 'linear', title: { display: true, text: 'Elapsed (s)' } },
                y: { title: { display: true, text: yLabel }, beginAtZero: true },
            },
        },
    });
    state.charts.push(chart);
}

function destroyCharts() {
    state.charts.forEach(c => c.destroy());
    state.charts = [];
}

// ── exports ─────────────────────────────────────────────────────────────────

async function exportExcel() {
    if (!state.selected.length) { toast('Select at least one run'); return; }
    try {
        const res = await fetch('/api/export/excel', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ run_ids: state.selected }),
        });
        if (!res.ok) throw new Error(res.status);
        downloadBlob(await res.blob(), 'mongo-ycsb-comparison.xlsx');
    } catch (e) { toast('Excel export failed'); }
}

async function exportPDF() {
    const el = document.getElementById('compare-content');
    if (!el || !state.selected.length) { toast('Select at least one run'); return; }
    toast('Rendering PDF…');
    try {
        const canvas = await html2canvas(el, { scale: 2, backgroundColor: '#ffffff' });
        const img = canvas.toDataURL('image/png');
        const { jsPDF } = window.jspdf;
        const pdf = new jsPDF('p', 'pt', 'a4');
        const pageW = pdf.internal.pageSize.getWidth();
        const pageH = pdf.internal.pageSize.getHeight();
        const imgH = canvas.height * pageW / canvas.width;
        let heightLeft = imgH, position = 0;
        pdf.addImage(img, 'PNG', 0, position, pageW, imgH);
        heightLeft -= pageH;
        while (heightLeft > 0) {
            position -= pageH;
            pdf.addPage();
            pdf.addImage(img, 'PNG', 0, position, pageW, imgH);
            heightLeft -= pageH;
        }
        pdf.save('mongo-ycsb-comparison.pdf');
        toast('PDF downloaded');
    } catch (e) { toast('PDF export failed'); }
}

function downloadBlob(blob, name) {
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = name;
    document.body.appendChild(a); a.click(); a.remove();
    URL.revokeObjectURL(url);
}

// ── small helpers ───────────────────────────────────────────────────────────

function section(title, inner) { return `<section class="cmp-section"><h3>${title}</h3>${inner}</section>`; }

function headRow(runs) {
    return `<thead><tr><th class="rowlabel">Metric</th>${runs.map((r, i) =>
        `<th><span class="dot" style="background:${palette[i % palette.length]}"></span>${shortId(r.run_id)}</th>`).join('')}</tr></thead>`;
}

function metricRow(label, values, lowerBetter, fmt) {
    const cls = (lowerBetter === null) ? values.map(() => '') : highlight(values, lowerBetter);
    const cells = values.map((v, i) => `<td class="${cls[i]}">${v == null ? '—' : fmt(v)}</td>`).join('');
    return `<tr><td class="rowlabel">${label}</td>${cells}</tr>`;
}

function highlight(values, lowerBetter) {
    const nums = values.map(v => (typeof v === 'number' && isFinite(v)) ? v : null);
    const valid = nums.filter(v => v !== null);
    if (valid.length < 2) return nums.map(() => '');
    const min = Math.min(...valid), max = Math.max(...valid);
    if (min === max) return nums.map(() => '');
    return nums.map(v => {
        if (v === null) return '';
        if (lowerBetter) return v === min ? 'best' : (v === max ? 'worst' : '');
        return v === max ? 'best' : (v === min ? 'worst' : '');
    });
}

function unionOps(runs) {
    const set = new Set();
    runs.forEach(r => Object.keys(r.summary?.by_operation || {}).forEach(op => set.add(op)));
    return [...set].sort();
}

function shortId(id) { return id ? id.slice(0, 8) : '—'; }
function esc(s) { return String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c])); }
function fmtNum(v, dec) { return (v == null || isNaN(v)) ? '—' : Number(v).toFixed(dec); }
function fmtOps(v) { return (v == null || isNaN(v)) ? '—' : Math.round(v).toLocaleString(); }
function fmtInt(v) { return (v == null || isNaN(v)) ? '—' : Number(v).toLocaleString(); }
function fmtTime(iso) {
    if (!iso) return '—';
    const d = new Date(iso);
    if (isNaN(d) || d.getUTCFullYear() < 1971) return '—';
    const p = n => String(n).padStart(2, '0');
    return `${d.getUTCFullYear()}-${p(d.getUTCMonth() + 1)}-${p(d.getUTCDate())} ${p(d.getUTCHours())}:${p(d.getUTCMinutes())}:${p(d.getUTCSeconds())} UTC`;
}

function toast(msg) {
    const t = document.getElementById('toast');
    t.textContent = msg; t.hidden = false;
    clearTimeout(window.__toastTimer);
    window.__toastTimer = setTimeout(() => { t.hidden = true; }, 2500);
}
