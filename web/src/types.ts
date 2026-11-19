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
// Same shape as detect.SpoofEvent in internal/detect/spoof.go
export type SpoofEvent = {
  time: string;
  kind: "teleport" | "identity_clash";
  mmsi: number;
  name: string;
  fromLat: number;
  fromLon: number;
  toLat: number;
  toLon: number;
  km: number;
  seconds: number;
  impliedKn: number;
  count?: number; // teleports in the window (identity_clash only)
};

// Same shape as detect.EncounterEvent in internal/detect/encounter.go
export type EncounterEvent = {
  time: string;
  kind: "encounter_start" | "encounter_end";
  mmsi1: number;
  name1: string;
  type1?: number;
  draught1?: number;
  mmsi2: number;
  name2: string;
  type2?: number;
  draught2?: number;
  lat: number;
  lon: number;
  minutes: number; // how long they've been together
};

// A ship's ID card, same shape as ais.ShipInfo in internal/ais/static.go
export type ShipInfo = {
  mmsi: number;
  name: string;
  type: number; // AIS ship type code, 0 = unknown
  draughtM: number; // 0 = unknown
  lengthM: number;
  destination: string;
  updated: string;
};
