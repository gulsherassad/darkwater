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

// Same shape as detect.Alert in internal/detect/monitor.go
export type Alert = {
  mmsi: number;
  name: string;
  level: "watch" | "alert";
  silentMinutes: number;
  judgedMinutes: number;
  chance: number;
  lastSeen: string;
  lastLat: number;
  lastLon: number;
  speedKn: number;
  courseDeg: number;
  projLat: number;
  projLon: number;
};

export type WsMessage = {
  type: "snapshot" | "update" | "alerts";
  ships?: Ship[];
  alerts?: Alert[];
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