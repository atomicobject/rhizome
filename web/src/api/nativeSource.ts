export type NativeSourceValue =
  | { kind: "null"; scalar?: null }
  | { kind: "string" | "date" | "timestamp"; scalar: string }
  | { kind: "integer" | "float"; scalar: number; scalarText?: string }
  | { kind: "boolean"; scalar: boolean }
  | { kind: "array"; items?: NativeSourceValue[] }
  | {
      kind: "object";
      entries?: Array<{ key: string | number; keyText?: string; value: NativeSourceValue }>;
    };

/** A source-value payload is read as native data; its text never enters Markdown. */
export type NativeSourceContent = {
  representation: "source_value";
  text: string;
  mediaType: string;
  value: NativeSourceValue;
  address?: string;
  sourceHash?: string;
  format?: string;
  providerVersion?: string;
  owner?: string;
  binding?: string;
};
