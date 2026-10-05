import { internalCall } from "#internal";
export type { LibraryRecord } from "./types.js";

export function libraryCall(): string {
  return internalCall();
}
