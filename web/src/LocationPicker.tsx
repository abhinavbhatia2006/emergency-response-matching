import { useCallback, useEffect, useRef, useState } from "react";
import * as L from "leaflet";
import { openStreetMapTiles } from "./mapTiles";

const pin = L.divIcon({ className: "location-pin", iconSize: [24, 32], iconAnchor: [12, 32] });

export default function LocationPicker() {
  const container = useRef<HTMLDivElement>(null);
  const map = useRef<L.Map | null>(null);
  const marker = useRef<L.Marker | null>(null);
  const latitude = useRef<HTMLInputElement>(null);
  const longitude = useRef<HTMLInputElement>(null);
  const locationAttempt = useRef(0);
  const [message, setMessage] = useState("");
  const [tileError, setTileError] = useState(false);

  // The named inputs remain the form's source of truth for all three input methods.
  const selectPoint = useCallback((point: L.LatLng, zoomIn = false) => {
    if (!map.current || !latitude.current || !longitude.current) return;
    locationAttempt.current++;
    setMessage("");
    const wrapped = Math.abs(point.lng) <= 180 ? point : point.wrap();
    latitude.current.value = String(wrapped.lat);
    longitude.current.value = String(wrapped.lng);
    if (marker.current) {
      marker.current.setLatLng(wrapped);
    } else {
      const next = L.marker(wrapped, { icon: pin, draggable: true, title: "Selected location" }).addTo(map.current);
      next.on("dragend", () => selectPoint(next.getLatLng()));
      marker.current = next;
    }
    if (zoomIn) map.current.setView(wrapped, Math.max(15, map.current.getZoom()));
  }, []);

  useEffect(() => {
    const element = container.current!;
    const instance = L.map(element, { scrollWheelZoom: false, worldCopyJump: true }).setView([20, 0], 2);
    map.current = instance;
    openStreetMapTiles(setTileError).addTo(instance);
    instance.on("click", (event: L.LeafletMouseEvent) => selectPoint(event.latlng));
    return () => {
      locationAttempt.current++;
      instance.remove();
      marker.current = null;
      map.current = null;
    };
  }, [selectPoint]);

  function locate() {
    const attempt = ++locationAttempt.current;
    if (!navigator.geolocation || !window.isSecureContext) {
      setMessage("Device location is unavailable here. Tap the map or enter coordinates.");
      return;
    }
    setMessage("Finding your location…");
    navigator.geolocation.getCurrentPosition(({ coords }) => {
      if (attempt === locationAttempt.current) selectPoint(L.latLng(coords.latitude, coords.longitude), true);
    }, error => {
      if (attempt !== locationAttempt.current) return;
      const reason = error.code === 1 ? "Location permission was denied." : "Your device could not locate you.";
      setMessage(reason + " Tap the map or enter coordinates.");
    }, { enableHighAccuracy: true, timeout: 10000 });
  }

  function editCoordinates() {
    locationAttempt.current++;
    setMessage("");
    marker.current?.remove();
    marker.current = null;
  }

  function previewCoordinates() {
    const lat = latitude.current!.valueAsNumber;
    const lng = longitude.current!.valueAsNumber;
    if (Number.isFinite(lat) && Math.abs(lat) <= 90 && Number.isFinite(lng) && Math.abs(lng) <= 180) {
      selectPoint(L.latLng(lat, lng), true);
    }
  }

  return <fieldset className="coordinate-fields location-picker" aria-describedby="coordinate-help">
    <legend>Pinned location</legend>
    <button type="button" className="secondary locate-button" onClick={locate}>Use my location</button>
    <p className="field-help picker-help">Tap the map to place a pin, drag it to adjust, or enter coordinates below.</p>
    <div className="location-map" role="region" aria-label="Location map" ref={container} />
    {tileError && <p className="field-help picker-help">Map tiles could not load. You can still use your device location or enter coordinates.</p>}
    {message && <p className="field-help picker-help" role="status">{message}</p>}
    <label>Latitude<input ref={latitude} name="latitude" type="number" required min={-90} max={90} step="any" inputMode="decimal" onChange={editCoordinates} onBlur={previewCoordinates} /></label>
    <label>Longitude<input ref={longitude} name="longitude" type="number" required min={-180} max={180} step="any" inputMode="decimal" onChange={editCoordinates} onBlur={previewCoordinates} /></label>
  </fieldset>;
}
