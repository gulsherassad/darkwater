// Same shape as shipJSON in internal/api/api.go
export type Ship = {
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

export type WsMessage = {
  type: "snapshot" | "update";
  ships: Ship[];
};

// One ship's recent track, for drawing trails
export type Trip = {
  mmsi: number;
  path: [number, number][]; // [lon, lat] points
  timestamps: number[]; // seconds since page load, one per point
};

// Same shape as coverageCell in internal/api/coverage.go
export type CoverageCell = {
  cell: string; // H3 cell ID
  dmaMsgs: number;
  dmaShips: number;
  feedMsgs: number;
  feedShips: number;
};