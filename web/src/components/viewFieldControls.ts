import type { ViewFieldCapability } from "../api/types";

export type ViewFieldControl = {
  kind: "enum" | "boolean" | "date" | "datetime" | "number" | "relation" | "text";
  ops: string[];
  defaultOp: string;
  options: string[];
  inputType: "text" | "date" | "datetime-local" | "number";
};

export function viewFieldControl(capability: ViewFieldCapability | undefined): ViewFieldControl {
  const valueKind = (capability?.valueKind || "").toLowerCase();
  const ops = capabilityFilterOps(capability);
  const options = capabilityOptions(capability);

  if (valueKind === "enum") {
    return {
      kind: "enum",
      ops,
      defaultOp: preferredOp(ops, "in"),
      options,
      inputType: "text",
    };
  }

  if (valueKind === "bool" || valueKind === "boolean") {
    return {
      kind: "boolean",
      ops,
      defaultOp: preferredOp(ops, "eq"),
      options: options.length ? options : ["false", "true"],
      inputType: "text",
    };
  }

  if (valueKind === "date") {
    return {
      kind: "date",
      ops,
      defaultOp: preferredOp(ops, "gte"),
      options,
      inputType: "date",
    };
  }

  if (valueKind === "datetime") {
    return {
      kind: "datetime",
      ops,
      defaultOp: preferredOp(ops, "gte"),
      options,
      inputType: "datetime-local",
    };
  }

  if (["int", "integer", "real", "float", "number"].includes(valueKind)) {
    return {
      kind: "number",
      ops,
      defaultOp: preferredOp(ops, "eq"),
      options,
      inputType: "number",
    };
  }

  if (valueKind === "relation") {
    return {
      kind: "relation",
      ops,
      defaultOp: preferredOp(ops, "eq"),
      options,
      inputType: "text",
    };
  }

  return {
    kind: "text",
    ops,
    defaultOp: options.length ? preferredOp(ops, "in") : preferredOp(ops, "contains"),
    options,
    inputType: "text",
  };
}

function capabilityFilterOps(capability: ViewFieldCapability | undefined) {
  const richOps = capability?.filter?.ops ?? [];

  if (richOps.length > 0) return richOps;

  if (capability?.filterOps?.length) return capability.filterOps;

  return ["contains"];
}

export function capabilityOptions(capability: ViewFieldCapability | undefined) {
  const enumValues =
    capability?.enumValues?.flatMap((value) => (value.value ? [value.value] : [])) ?? [];

  if (enumValues.length > 0) return enumValues;
  const filterOptions = capability?.filter?.options ?? [];

  if (filterOptions.length > 0)
    return filterOptions.flatMap((value) => {
      const option = String(value);

      return option ? [option] : [];
    });
  const facetValues = capability?.facets?.values ?? capability?.values ?? [];

  const options = facetValues.flatMap((value) => {
    const option = String(value);

    return option ? [option] : [];
  });

  if (options.length > 0) return options;

  return capability?.edit?.options?.flatMap((value) => (value ? [value] : [])) ?? [];
}

function preferredOp(ops: string[], preferred: string) {
  if (ops.includes(preferred)) return preferred;

  if (preferred === "gte" && ops.includes("eq")) return "eq";

  return ops[0] ?? preferred;
}
