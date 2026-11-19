import { tileLayer } from "leaflet";
import "leaflet/dist/leaflet.css";

export function openStreetMapTiles(setTileError: (failed: boolean) => void) {
  return tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
    maxZoom: 19,
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap contributors</a>',
  }).on("loading", () => setTileError(false)).on("tileerror", () => setTileError(true));
}
