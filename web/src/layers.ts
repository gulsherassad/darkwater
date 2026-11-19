import { ScatterplotLayer, IconLayer, LineLayer } from "@deck.gl/layers";
import { TripsLayer, H3HexagonLayer } from "@deck.gl/geo-layers";
import type { Alert, CoverageCell, Ship, Trip } from "./types";

export const TRAIL_SECONDS = 30 * 60; // trails show the last 30 minutes

type RGB = [number, number, number];
const BLUE: RGB = [80, 200, 255];
const GREY: RGB = [120, 130, 145];
export const AMBER: RGB = [255, 180, 60]; // watch
export const RED: RGB = [255, 80, 80]; // alert

// A simple arrow pointing up (north). White, so deck.gl can recolour it.
const ARROW_URL =
  "data:image/svg+xml;utf8," +
  encodeURIComponent(
    '<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32">' +
      '<path d="M16 2 L27 28 L16 22 L5 28 Z" fill="white"/></svg>'
  );

// Coverage colours (keep in sync with the legend in style.css)
const BOTH: RGB = [45, 200, 160]; // teal
const DMA_ONLY: RGB = [140, 110, 230]; // violet
const FEED_ONLY: RGB = [240, 170, 60]; // amber

function coverageColor(c: CoverageCell): [number, number, number, number] {
  const base = c.dmaShips > 0 && c.feedShips > 0 ? BOTH : c.dmaShips > 0 ? DMA_ONLY : FEED_ONLY;
  // Brighter = more ships. Log scale so busy cells don't wash out quiet ones.
  const ships = Math.max(c.dmaShips, c.feedShips);
  const alpha = Math.round(40 + 160 * Math.min(1, Math.log1p(ships) / Math.log1p(100)));
  return [...base, alpha];
}

export function levelColor(level: Alert["level"]): RGB {
  return level === "alert" ? RED : AMBER;
}

// Which way the ship points: heading if known, otherwise direction of travel.
export function direction(s: Ship): number | null {
  if (s.heading >= 0) return s.heading;
  if (s.courseDeg >= 0) return s.courseDeg;
  return null;
}

export function isMoving(s: Ship): boolean {
  return s.speedKn > 0.5 && direction(s) !== null;
}

type LayerInput = {
  coverage: CoverageCell[] | null; // null = hidden
  moving: Ship[];
  stationary: Ship[];
  trips: Trip[];
  alerts: Map<number, Alert>;
  alertList: Alert[];
  alertsVersion: number; // bumps whenever alerts change, so colours get recomputed
  currentTime: number;
  selected: Ship | null;
};

export function buildLayers(input: LayerInput) {
  const { coverage, moving, stationary, trips, alerts, alertList, alertsVersion, currentTime, selected } = input;
  const colorFor = (s: Ship, base: RGB): RGB => {
    const a = alerts.get(s.mmsi);
    return a ? levelColor(a.level) : base;
  };

  return [
    // Coverage hexagons, drawn first so everything else sits on top
    new H3HexagonLayer<CoverageCell>({
      id: "coverage",
      data: coverage ?? [],
      visible: coverage !== null,
      getHexagon: (c) => c.cell,
      getFillColor: coverageColor,
      extruded: false,
      stroked: true,
      getLineColor: [255, 255, 255, 30],
      lineWidthMinPixels: 0.5,
      pickable: true,
    }),

    // Trails
    new TripsLayer<Trip>({
      id: "trails",
      data: trips,
      getPath: (t) => t.path,
      getTimestamps: (t) => t.timestamps,
      getColor: BLUE,
      opacity: 0.5,
      widthMinPixels: 2,
      fadeTrail: true,
      trailLength: TRAIL_SECONDS,
      currentTime,
    }),

    // Alerts: line from last seen to probable position now
    new LineLayer<Alert>({
      id: "alert-paths",
      data: alertList,
      getSourcePosition: (a) => [a.lastLon, a.lastLat],
      getTargetPosition: (a) => [a.projLon, a.projLat],
      getColor: (a) => [...levelColor(a.level), 200],
      getWidth: 2,
      widthUnits: "pixels",
    }),

    // Stationary ships: small grey dots
    new ScatterplotLayer<Ship>({
      id: "stationary",
      data: stationary,
      getPosition: (s) => [s.lon, s.lat],
      getRadius: 3,
      radiusUnits: "pixels",
      getFillColor: (s) => colorFor(s, GREY),
      updateTriggers: { getFillColor: alertsVersion },
      pickable: true,
    }),

    // Moving ships: arrows rotated to their direction
    new IconLayer<Ship>({
      id: "moving",
      data: moving,
      getPosition: (s) => [s.lon, s.lat],
      getIcon: () => ({ id: "arrow", url: ARROW_URL, width: 32, height: 32, mask: true }),
      getSize: 16,
      sizeUnits: "pixels",
      // Compass degrees go clockwise, deck.gl rotates counter-clockwise: flip the sign
      getAngle: (s) => -(direction(s) ?? 0),
      getColor: (s) => colorFor(s, BLUE),
      updateTriggers: { getColor: alertsVersion },
      pickable: true,
    }),

    // Alerts: ring at the probable position now
    new ScatterplotLayer<Alert>({
      id: "alert-rings",
      data: alertList,
      getPosition: (a) => [a.projLon, a.projLat],
      getRadius: 10,
      radiusUnits: "pixels",
      filled: false,
      stroked: true,
      getLineColor: (a) => levelColor(a.level),
      lineWidthMinPixels: 2,
      pickable: true,
    }),

    // White ring around the selected ship
    new ScatterplotLayer<Ship>({
      id: "selected",
      data: selected ? [selected] : [],
      getPosition: (s) => [s.lon, s.lat],
      getRadius: 14,
      radiusUnits: "pixels",
      filled: false,
      stroked: true,
      getLineColor: [255, 255, 255],
      lineWidthMinPixels: 2,
    }),
  ];
}