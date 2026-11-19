import * as maplibregl from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
import { MapboxOverlay } from "@deck.gl/mapbox";
import "./style.css";

import type { Alert, CoverageCell, EncounterEvent, Ship, ShipInfo, SpoofEvent, Trip, WsMessage } from "./types";
import { buildLayers, CYAN, isMoving, levelColor, PURPLE, TRAIL_SECONDS } from "./layers";

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
let spoofList: SpoofEvent[] = []; // newest first
let teleports: SpoofEvent[] = []; // the teleports in spoofList, for the map
let openEncounters: EncounterEvent[] = []; // start events with no end yet
let endedEncounters: EncounterEvent[] = []; // recent end events, newest first
let shipInfo: { mmsi: number; card: ShipInfo | null } | null = null; // ID card of the selected ship

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
    select(o && "mmsi" in o ? (o as Ship | Alert).mmsi : null);
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
      teleports,
      openEncounters,
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
    hud.textContent = `${ships.size} in view · ${moving.length} moving · ${alerts.size} flagged · ${spoofList.length} spoof · ${openEncounters.length} encounters · ${kbps} KB/s`;
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
const hhmm = (iso: string) => `${new Date(iso).toISOString().slice(11, 16)} UTC`;
const byNewest = (a: { time: string }, b: { time: string }) => Date.parse(b.time) - Date.parse(a.time);

// ---- Sidebar: dark ships, spoofing, encounters ----
function section(title: string, items: string[]) {
  const body = items.length > 0 ? items.join("") : `<li class="empty">none yet</li>`;
  return `<h3>${title} (${items.length})</h3><ul>${body}</ul>`;
}

function renderAlerts() {
  const dark = alertList.map(
    (a) => `
      <li data-kind="dark" data-mmsi="${a.mmsi}">
        <span class="dot" style="background:${rgb(levelColor(a.level))}"></span>
        <strong>${esc(a.name || "Unknown")}</strong>
        <small>${esc(a.level)} · silent ${a.silentMinutes} min · ${esc(oneIn(a.chance))}</small>
      </li>`
  );

  const spoof = spoofList.map(
    (e, i) => `
      <li data-kind="spoof" data-i="${i}">
        <span class="dot" style="background:${rgb(PURPLE)}"></span>
        <strong>${esc(e.name || e.mmsi)}</strong>
        <small>${esc(e.kind.replace("_", " "))} · ${esc(Math.round(e.impliedKn))} kn${
          e.count ? ` · ${esc(e.count)} jumps` : ""
        } · ${esc(hhmm(e.time))}</small>
      </li>`
  );

  const draught = (e: EncounterEvent) =>
    e.draught1 || e.draught2 ? ` · draught ${esc(e.draught1 || "?")}/${esc(e.draught2 || "?")} m` : "";
  const encounter = (e: EncounterEvent, i: number, open: boolean) => {
    // A start event holds the time together so far; add the time since then
    const minutes = open ? e.minutes + (Date.now() - Date.parse(e.time)) / 60000 : e.minutes;
    return `
      <li data-kind="${open ? "open" : "ended"}" data-i="${i}">
        <span class="dot" style="background:${open ? rgb(CYAN) : "#4a5866"}"></span>
        <strong>${esc(e.name1 || e.mmsi1)} &amp; ${esc(e.name2 || e.mmsi2)}</strong>
        <small>${open ? "together" : "ended"} · ${esc(Math.round(minutes))} min${draught(e)} · ${esc(hhmm(e.time))}</small>
      </li>`;
  };
  const encounters = [
    ...openEncounters.map((e, i) => encounter(e, i, true)),
    ...endedEncounters.map((e, i) => encounter(e, i, false)),
  ];

  alertsBox.hidden = false;
  alertsBox.innerHTML = section("Dark", dark) + section("Spoofing", spoof) + section("Encounters", encounters);
}

alertsBox.addEventListener("click", (e) => {
  const li = (e.target as HTMLElement).closest("li");
  if (!li) return;
  const i = Number(li.dataset.i);
  switch (li.dataset.kind) {
    case "dark": {
      const a = alerts.get(Number(li.dataset.mmsi));
      if (!a) return;
      map.flyTo({ center: [a.projLon, a.projLat], zoom: 9 });
      select(a.mmsi);
      break;
    }
    case "spoof": {
      const ev = spoofList[i];
      if (ev) map.flyTo({ center: [ev.toLon, ev.toLat], zoom: 9 });
      break;
    }
    case "open":
    case "ended": {
      const ev = (li.dataset.kind === "open" ? openEncounters : endedEncounters)[i];
      if (ev) map.flyTo({ center: [ev.lon, ev.lat], zoom: 11 });
      break;
    }
  }
});

// ---- Spoofing & encounters: poll every 30 s ----
const pairKey = (e: EncounterEvent) => `${Math.min(e.mmsi1, e.mmsi2)}-${Math.max(e.mmsi1, e.mmsi2)}`;

async function fetchList<T>(url: string): Promise<T[] | null> {
  try {
    const res = await fetch(url);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const data = await res.json();
    return Array.isArray(data) ? data : []; // Go sends null for an empty list
  } catch (err) {
    console.warn(`poll ${url} failed:`, err);
    return null; // keep what we had
  }
}

async function pollEvents() {
  const [spoof, enc] = await Promise.all([
    fetchList<SpoofEvent>("/api/spoof"),
    fetchList<EncounterEvent>("/api/encounters"),
  ]);
  if (spoof) {
    spoofList = [...spoof].sort(byNewest);
    teleports = spoofList.filter((e) => e.kind === "teleport");
  }
  if (enc) {
    // Latest event per pair: if it's a start, the encounter is still open
    const latest = new Map<string, EncounterEvent>();
    // Oldest first (stable sort keeps server order on equal times), so later events overwrite earlier ones
    const oldestFirst = [...enc].sort((a, b) => Date.parse(a.time) - Date.parse(b.time));
    for (const e of oldestFirst) latest.set(pairKey(e), e);
    const last = Array.from(latest.values()).sort(byNewest);
    openEncounters = last.filter((e) => e.kind === "encounter_start");
    endedEncounters = last.filter((e) => e.kind === "encounter_end").slice(0, 20);
  }
  renderAlerts();
}

pollEvents();
setInterval(pollEvents, 30_000);

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

function shipType(code: number): string {
  const exact: Record<number, string> = {
    30: "Fishing",
    31: "Towing",
    32: "Towing",
    35: "Military",
    36: "Sailing",
    37: "Pleasure craft",
    50: "Pilot",
    51: "Search & rescue",
    52: "Tug",
    53: "Port tender",
    55: "Law enforcement",
  };
  if (exact[code]) return exact[code];
  if (code >= 60 && code <= 69) return "Passenger";
  if (code >= 70 && code <= 79) return "Cargo";
  if (code >= 80 && code <= 89) return "Tanker";
  return `Other (code ${code})`;
}

// Change the selected ship and fetch its ID card
function select(mmsi: number | null) {
  selected = mmsi;
  if (mmsi !== null && shipInfo?.mmsi !== mmsi) {
    shipInfo = null;
    fetch(`/api/vessels/${mmsi}`)
      .then((res) => {
        if (res.status === 404) return null; // no ID card yet
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json() as Promise<ShipInfo>;
      })
      .then((card) => {
        if (selected !== mmsi) return; // user has moved on to another ship
        shipInfo = { mmsi, card };
        updatePanel();
      })
      .catch((err) => console.warn(`vessel ${mmsi} lookup failed:`, err));
  }
  updatePanel();
}

function idCardRows(): string {
  if (!shipInfo || shipInfo.mmsi !== selected) return ""; // still loading
  const c = shipInfo.card;
  if (!c) return `<dt>ID card</dt><dd>not received yet</dd>`;
  let rows = `
      <dt>Type</dt><dd>${esc(shipType(c.type))}</dd>
      <dt>Length</dt><dd>${esc(c.lengthM)} m</dd>`;
  if (c.draughtM > 0) rows += `<dt>Draught</dt><dd>${esc(c.draughtM)} m</dd>`;
  if (c.destination) rows += `<dt>Destination</dt><dd>${esc(c.destination)}</dd>`;
  return rows;
}

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
  rows += idCardRows();
  panel.hidden = false;
  panel.innerHTML = `
    <button id="close" aria-label="Close">×</button>
    <h2>${esc(name)}</h2>
    <dl>${rows}</dl>`;
}

panel.addEventListener("click", (e) => {
  if ((e.target as HTMLElement).id === "close") select(null);
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