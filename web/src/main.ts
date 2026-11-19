import * as maplibregl from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
import { MapboxOverlay } from "@deck.gl/mapbox";
import "./style.css";

import type { Alert, CoverageCell, Ship, Trip, WsMessage } from "./types";
import { buildLayers, isMoving, levelColor, TRAIL_SECONDS } from "./layers";

const hud = document.getElementById("hud")!;
const panel = document.getElementById("panel")!;
const alertsBox = document.getElementById("alerts")!;
const toggle = document.getElementById("toggle-coverage") as HTMLButtonElement;
const legend = document.getElementById("legend")!;

// ---- Time: seconds since page load (small numbers, safe for the GPU) ----
const BASE = Math.floor(Date.now() / 1000);
const nowT = () => Date.now() / 1000 - BASE;
const toT = (iso: string) => Date.parse(iso) / 1000 - BASE;

// ---- State ----
const ships = new Map<number, Ship>(); // MMSI -> latest report (in view)
const history = new Map<number, Trip>(); // MMSI -> recent track
const alerts = new Map<number, Alert>(); // MMSI -> current alert
let alertList: Alert[] = []; // same alerts, most suspicious first
let alertsVersion = 0;
let selected: number | null = null;
let socket: WebSocket | null = null;
let bytesThisSecond = 0;
let coverage: CoverageCell[] = [];
let showCoverage = false;

// Cached lists, rebuilt only when data changes (not every frame)
let moving: Ship[] = [];
let stationary: Ship[] = [];
let trips: Trip[] = [];

function addToHistory(s: Ship) {
  let trip = history.get(s.mmsi);
  if (!trip) {
    trip = { mmsi: s.mmsi, path: [], timestamps: [] };
    history.set(s.mmsi, trip);
  }
  const last = trip.path[trip.path.length - 1];
  if (!last || last[0] !== s.lon || last[1] !== s.lat) {
    trip.path.push([s.lon, s.lat]);
    trip.timestamps.push(toT(s.time));
  }
  // Drop points older than the trail window
  const cutoff = nowT() - TRAIL_SECONDS;
  while (trip.timestamps.length > 0 && trip.timestamps[0] < cutoff) {
    trip.path.shift();
    trip.timestamps.shift();
  }
}

function rebuildData() {
  moving = [];
  stationary = [];
  for (const s of ships.values()) {
    (isMoving(s) ? moving : stationary).push(s);
  }
  // A trail needs at least 2 points to draw a line
  trips = Array.from(history.values()).filter((t) => t.path.length >= 2);
}

// ---- Map ----
const map = new maplibregl.Map({
  container: "map",
  style: "https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json",
  center: [11, 56], // [lon, lat], centred on Denmark
  zoom: 6,
});

const overlay = new MapboxOverlay({
  layers: [],
  getTooltip: ({ object }) => {
    if (!object) return null;
    if ("cell" in object) {
      const c = object as CoverageCell;
      return `Cell ${c.cell}\nDMA: ${c.dmaShips} ships · ${c.dmaMsgs} msgs\nFeed: ${c.feedShips} ships · ${c.feedMsgs} msgs`;
    }
    if ("projLat" in object) {
      const a = object as Alert;
      return `${a.name || "Unknown"} — ${a.level}\nsilent ${a.silentMinutes} min · ${oneIn(a.chance)}\nprobable position now`;
    }
    return `${(object as Ship).name || "Unknown"} (click for details)`;
  },
  onClick: (info) => {
    // Clicking a ship or alert ring selects it; clicking anything else clears the selection
    const o = info.object;
    selected = o && "mmsi" in o ? (o as Ship | Alert).mmsi : null;
    updatePanel();
  },
});
map.addControl(overlay);

// ---- Coverage: load once, toggle with the button ----
fetch("/api/coverage")
  .then((res) => (res.ok ? res.json() : []))
  .then((cells: CoverageCell[]) => {
    coverage = cells;
  })
  .catch(() => {
    coverage = [];
  });

toggle.addEventListener("click", () => {
  showCoverage = !showCoverage;
  legend.hidden = !showCoverage;
  toggle.textContent = showCoverage ? "Hide coverage" : "Show coverage";
});

// ---- Viewport: tell the server what we're looking at ----
function viewport() {
  const b = map.getBounds();
  // Pad by 20% so ships just off-screen are already loaded when panning
  const padX = (b.getEast() - b.getWest()) * 0.2;
  const padY = (b.getNorth() - b.getSouth()) * 0.2;
  return {
    type: "viewport",
    west: Math.max(-180, b.getWest() - padX),
    south: Math.max(-90, b.getSouth() - padY),
    east: Math.min(180, b.getEast() + padX),
    north: Math.min(90, b.getNorth() + padY),
  };
}

function sendViewport() {
  if (socket && socket.readyState === WebSocket.OPEN) {
    socket.send(JSON.stringify(viewport()));
  }
}

// moveend fires once when panning/zooming stops, not on every frame of the drag
map.on("moveend", sendViewport);

// ---- Animation: only the clock moves each frame ----
function frame() {
  overlay.setProps({
    layers: buildLayers({
      coverage: showCoverage ? coverage : null,
      moving,
      stationary,
      trips,
      alerts,
      alertList,
      alertsVersion,
      currentTime: nowT(),
      selected: selected !== null ? ships.get(selected) ?? null : null,
    }),
  });
  requestAnimationFrame(frame);
}
requestAnimationFrame(frame);

// ---- HUD: ships in view, flagged ships, data rate ----
setInterval(() => {
  if (socket && socket.readyState === WebSocket.OPEN) {
    const kbps = (bytesThisSecond / 1024).toFixed(1);
    hud.textContent = `${ships.size} in view · ${moving.length} moving · ${alerts.size} flagged · ${kbps} KB/s`;
  }
  bytesThisSecond = 0;
}, 1000);

// ---- Helpers ----
// Ship data comes from open radio: escape it before putting it in HTML
function esc(v: unknown): string {
  return String(v).replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!
  );
}

const fmt = (v: number, unit: string) => (v < 0 ? "unknown" : `${v}${unit}`);
const oneIn = (chance: number) => `1 in ${Math.round(1 / Math.max(chance, 1e-12)).toLocaleString()}`;
const rgb = (c: [number, number, number]) => `rgb(${c[0]}, ${c[1]}, ${c[2]})`;

// ---- Alerts list ----
function renderAlerts() {
  if (alertList.length === 0) {
    alertsBox.hidden = true;
    return;
  }
  alertsBox.hidden = false;
  alertsBox.innerHTML =
    `<h3>Flagged ships (${alertList.length})</h3><ul>` +
    alertList
      .map(
        (a) => `
      <li data-mmsi="${a.mmsi}">
        <span class="dot" style="background:${rgb(levelColor(a.level))}"></span>
        <strong>${esc(a.name || "Unknown")}</strong>
        <small>${esc(a.level)} · silent ${a.silentMinutes} min · ${esc(oneIn(a.chance))}</small>
      </li>`
      )
      .join("") +
    `</ul>`;
}

alertsBox.addEventListener("click", (e) => {
  const li = (e.target as HTMLElement).closest("li");
  if (!li) return;
  const a = alerts.get(Number(li.dataset.mmsi));
  if (!a) return;
  selected = a.mmsi;
  map.flyTo({ center: [a.projLon, a.projLat], zoom: 9 });
  updatePanel();
});

// ---- Details panel ----
const NAV_STATUS: Record<number, string> = {
  0: "Under way (engine)",
  1: "At anchor",
  2: "Not under command",
  3: "Restricted manoeuvrability",
  4: "Constrained by draught",
  5: "Moored",
  6: "Aground",
  7: "Fishing",
  8: "Under way (sailing)",
  15: "Not defined",
};

function updatePanel() {
  const s = selected !== null ? ships.get(selected) : undefined;
  const a = selected !== null ? alerts.get(selected) : undefined;
  if (!s && !a) {
    panel.hidden = true;
    return;
  }
  const name = s?.name || a?.name || "Unknown";
  let rows = "";
  if (a) {
    rows += `
      <dt>Flag</dt><dd style="color:${rgb(levelColor(a.level))}">${esc(a.level.toUpperCase())}</dd>
      <dt>Silent</dt><dd>${esc(a.silentMinutes)} min (${esc(a.judgedMinutes)} in known coverage)</dd>
      <dt>Chance</dt><dd>${esc(oneIn(a.chance))} that bad reception explains it</dd>`;
  }
  if (s) {
    const ago = Math.max(0, Math.round((Date.now() - Date.parse(s.time)) / 1000));
    rows += `
      <dt>MMSI</dt><dd>${esc(s.mmsi)}</dd>
      <dt>Speed</dt><dd>${esc(fmt(s.speedKn, " kn"))}</dd>
      <dt>Course</dt><dd>${esc(fmt(s.courseDeg, "°"))}</dd>
      <dt>Heading</dt><dd>${esc(fmt(s.heading, "°"))}</dd>
      <dt>Status</dt><dd>${esc(NAV_STATUS[s.navStatus] ?? s.navStatus)} <small>(self-reported)</small></dd>
      <dt>Position</dt><dd>${esc(s.lat.toFixed(4))}, ${esc(s.lon.toFixed(4))}</dd>
      <dt>Last report</dt><dd>${esc(ago)}s ago</dd>`;
  } else if (a) {
    rows += `<dt>MMSI</dt><dd>${esc(a.mmsi)}</dd>
      <dt>Last seen</dt><dd>${esc(a.lastLat.toFixed(4))}, ${esc(a.lastLon.toFixed(4))}</dd>`;
  }
  panel.hidden = false;
  panel.innerHTML = `
    <button id="close" aria-label="Close">×</button>
    <h2>${esc(name)}</h2>
    <dl>${rows}</dl>`;
}

panel.addEventListener("click", (e) => {
  if ((e.target as HTMLElement).id === "close") {
    selected = null;
    updatePanel();
  }
});
setInterval(updatePanel, 1000); // keeps "last report" ticking

// ---- Live data ----
function connect() {
  const protocol = location.protocol === "https:" ? "wss" : "ws";
  socket = new WebSocket(`${protocol}://${location.host}/api/ws`);

  socket.onopen = sendViewport; // tell the server our view right away

  socket.onmessage = (event) => {
    bytesThisSecond += event.data.length;
    const msg: WsMessage = JSON.parse(event.data);

    if (msg.type === "alerts") {
      alerts.clear();
      alertList = msg.alerts ?? [];
      for (const a of alertList) alerts.set(a.mmsi, a);
      alertsVersion++; // tells deck.gl to recompute ship colours
      renderAlerts();
      updatePanel();
      return;
    }

    if (msg.type === "snapshot") {
      ships.clear();
    }
    for (const s of msg.ships ?? []) {
      ships.set(s.mmsi, s);
      addToHistory(s);
    }
    if (msg.type === "snapshot") {
      // Keep trails for ships still in view, forget the rest
      for (const mmsi of history.keys()) {
        if (!ships.has(mmsi)) history.delete(mmsi);
      }
    }

    rebuildData();
    updatePanel();
  };

  socket.onclose = () => {
    hud.textContent = "reconnecting…";
    setTimeout(connect, 2000);
  };
}

connect();