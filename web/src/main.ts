import * as maplibregl from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
import { MapboxOverlay } from "@deck.gl/mapbox";
import "./style.css";

import type { Ship, Trip, WsMessage } from "./types";
import { buildLayers, isMoving, TRAIL_SECONDS } from "./layers";

const hud = document.getElementById("hud")!;
const panel = document.getElementById("panel")!;

// ---- Time: seconds since page load (small numbers, safe for the GPU) ----
const BASE = Math.floor(Date.now() / 1000);
const nowT = () => Date.now() / 1000 - BASE;
const toT = (iso: string) => Date.parse(iso) / 1000 - BASE;

// ---- State ----
const ships = new Map<number, Ship>(); // MMSI -> latest report
const history = new Map<number, Trip>(); // MMSI -> recent track
let selected: number | null = null;

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
  hud.textContent = `${ships.size} ships · ${moving.length} moving · live`;
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
  getTooltip: ({ object }) => object && `${object.name || "Unknown"} (click for details)`,
  onClick: (info) => {
    // Clicking a ship selects it; clicking empty water clears the selection
    selected = info.object ? (info.object as Ship).mmsi : null;
    updatePanel();
  },
});
map.addControl(overlay);

// ---- Animation: only the clock moves each frame ----
function frame() {
  overlay.setProps({
    layers: buildLayers({
      moving,
      stationary,
      trips,
      currentTime: nowT(),
      selected: selected !== null ? ships.get(selected) ?? null : null,
    }),
  });
  requestAnimationFrame(frame);
}
requestAnimationFrame(frame);

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

// Ship data comes from open radio: escape it before putting it in HTML
function esc(v: unknown): string {
  return String(v).replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!
  );
}

const fmt = (v: number, unit: string) => (v < 0 ? "unknown" : `${v}${unit}`);

function updatePanel() {
  const s = selected !== null ? ships.get(selected) : undefined;
  if (!s) {
    panel.hidden = true;
    return;
  }
  const ago = Math.max(0, Math.round((Date.now() - Date.parse(s.time)) / 1000));
  panel.hidden = false;
  panel.innerHTML = `
    <button id="close" aria-label="Close">×</button>
    <h2>${esc(s.name || "Unknown")}</h2>
    <dl>
      <dt>MMSI</dt><dd>${esc(s.mmsi)}</dd>
      <dt>Speed</dt><dd>${esc(fmt(s.speedKn, " kn"))}</dd>
      <dt>Course</dt><dd>${esc(fmt(s.courseDeg, "°"))}</dd>
      <dt>Heading</dt><dd>${esc(fmt(s.heading, "°"))}</dd>
      <dt>Status</dt><dd>${esc(NAV_STATUS[s.navStatus] ?? s.navStatus)} <small>(self-reported)</small></dd>
      <dt>Position</dt><dd>${esc(s.lat.toFixed(4))}, ${esc(s.lon.toFixed(4))}</dd>
      <dt>Last report</dt><dd>${esc(ago)}s ago</dd>
    </dl>`;
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
  const ws = new WebSocket(`${protocol}://${location.host}/api/ws`);

  ws.onmessage = (event) => {
    const msg: WsMessage = JSON.parse(event.data);
    if (msg.type === "snapshot") {
      ships.clear();
      history.clear();
    }
    for (const s of msg.ships) {
      ships.set(s.mmsi, s);
      addToHistory(s);
    }
    rebuildData();
    updatePanel();
  };

  ws.onclose = () => {
    hud.textContent = "reconnecting…";
    setTimeout(connect, 2000);
  };
}

connect();