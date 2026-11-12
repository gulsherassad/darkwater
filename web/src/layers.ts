import { ScatterplotLayer, IconLayer } from "@deck.gl/layers";
import { TripsLayer } from "@deck.gl/geo-layers";
import type { Ship, Trip } from "./types";

export const TRAIL_SECONDS = 30 * 60; // trails show the last 30 minutes

// A simple arrow pointing up (north). White, so deck.gl can recolour it.
const ARROW_URL =
  "data:image/svg+xml;utf8," +
  encodeURIComponent(
    '<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32">' +
      '<path d="M16 2 L27 28 L16 22 L5 28 Z" fill="white"/></svg>'
  );

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
  moving: Ship[];
  stationary: Ship[];
  trips: Trip[];
  currentTime: number;
  selected: Ship | null;
};

export function buildLayers({ moving, stationary, trips, currentTime, selected }: LayerInput) {
  return [
    // Trails, drawn first so ships sit on top of them
    new TripsLayer<Trip>({
      id: "trails",
      data: trips,
      getPath: (t) => t.path,
      getTimestamps: (t) => t.timestamps,
      getColor: [80, 200, 255],
      opacity: 0.5,
      widthMinPixels: 2,
      fadeTrail: true,
      trailLength: TRAIL_SECONDS,
      currentTime,
    }),

    // Stationary ships: small grey dots
    new ScatterplotLayer<Ship>({
      id: "stationary",
      data: stationary,
      getPosition: (s) => [s.lon, s.lat],
      getRadius: 3,
      radiusUnits: "pixels",
      getFillColor: [120, 130, 145],
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
      getColor: [80, 200, 255],
      pickable: true,
    }),

    // White ring around the selected ship
    new ScatterplotLayer<Ship>({
      id: "selected",
      data: selected ? [selected] : [],
      getPosition: (s) => [s.lon, s.lat],
      getRadius: 12,
      radiusUnits: "pixels",
      filled: false,
      stroked: true,
      getLineColor: [255, 255, 255],
      lineWidthMinPixels: 2,
    }),
  ];
}