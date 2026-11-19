export type Kind = "food" | "water" | "medicine" | "transportation" | "shelter";
export type Coordinates = { latitude: number; longitude: number };

export type RequestRecord = {
  id: string;
  type: Kind;
  location: string;
  directions?: string;
  details: string;
  status: "pending" | "matched" | "completed" | "cancelled";
  created_at: string;
  resource_id?: string;
  coordinates?: Coordinates;
  distance_meters?: number;
};

export type Resource = {
  id: string;
  type: Kind;
  location: string;
  directions?: string;
  availability: boolean;
  status: "available" | "unavailable" | "reserved";
  coordinates?: Coordinates;
};

export const title = (value: string) => value.charAt(0).toUpperCase() + value.slice(1);
export const kilometers = (meters: number) => `${new Intl.NumberFormat("en", { maximumFractionDigits: 3 }).format(meters / 1000)} km`;

export function validCoordinates(point?: Coordinates): point is Coordinates {
  return !!point && Number.isFinite(point.latitude) && Math.abs(point.latitude) <= 90
    && Number.isFinite(point.longitude) && Math.abs(point.longitude) <= 180;
}
