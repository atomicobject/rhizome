import type { JsonValue } from "./parse";

/** A source-value payload is read as native data; its text never enters Markdown. */
export type NativeSourceContent = {
  representation: "source_value";
  text: string;
  mediaType: string;
  value: JsonValue;
  address?: string;
  sourceHash?: string;
  format?: string;
  providerVersion?: string;
  owner?: string;
  binding?: string;
};
