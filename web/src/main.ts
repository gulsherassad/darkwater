import * as maplibregl from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
import { MapboxOverlay } from "@deck.gl/mapbox";
import { ScatterplotLayer } from "@deck.gl/layers";
import "./style.css";

// Same shape as shipJSON in internal/api/api.go
type Ship = {
  mmsi: number;
  name: string;
  time: string;
  lat: number;
  lon: number;
  speedKn: number; // -1 = unknown
  courseDeg: number; // -1 = unknown
  heading: number; // -1 = unknown
  navStatus: number;
};

type WsMessage = {
  type: "snapshot" | "update";
  ships: Ship[];
};

const hud = document.getElementById("hud")!;

// The browser's own whiteboard: MMSI -> latest ship
const ships = new Map<number, Ship>();

// Bottom sheet: the base map
const map = new maplibregl.Map({
  container: "map",
  style: "https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json",
  center: [11, 56], // [lon, lat], centred on Denmark
  zoom: 6,
});

// Top sheet: deck.gl draws the ships
const overlay = new MapboxOverlay({
  layers: [],
  getTooltip: ({ object }) =>
    object && `${object.name || "Unknown"}\nMMSI ${object.mmsi}\n${object.speedKn} kn`,
});
map.addControl(overlay);

function render() {
  overlay.setProps({
    layers: [
      new ScatterplotLayer<Ship>({
        id: "ships",
        data: Array.from(ships.values()),
        getPosition: (s) => [s.lon, s.lat], // deck.gl wants [lon, lat]
        getRadius: 4,
        radiusUnits: "pixels",
        // Moving ships bright blue, stationary ones grey
        getFillColor: (s) => (s.speedKn > 0.5 ? [80, 200, 255] : [120, 130, 145]),
        pickable: true,
      }),
    ],
  });
  hud.textContent = `${ships.size} ships · live`;
}

function connect() {
  const protocol = location.protocol === "https:" ? "wss" : "ws";
  const ws = new WebSocket(`${protocol}://${location.host}/api/ws`);

  ws.onmessage = (event) => {
    const msg: WsMessage = JSON.parse(event.data);
    if (msg.type === "snapshot") {
      ships.clear(); // fresh start: replace everything
    }
    for (const s of msg.ships) {
      ships.set(s.mmsi, s); // add new ship or overwrite existing one
    }
    render();
  };

  // Connection lost: say so, then retry in 2 seconds
  ws.onclose = () => {
    hud.textContent = "reconnecting…";
    setTimeout(connect, 2000);
  };
}

connect();