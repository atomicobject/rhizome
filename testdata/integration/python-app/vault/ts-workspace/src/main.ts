import { libraryCall } from "@fixture/library";
import type { LibraryRecord } from "@fixture/library";
import { aliasCall } from "@ui/button";
import { barrelCall } from "./barrel.js";
import { plainCall } from "./plain.js";
import { runtimeCall } from "./runtime.js";
import { viewCall } from "./view.js";
import { widgetCall } from "./widget.jsx";
import { esmCall } from "./native-esm.mjs";
import "./setup.js";

const legacy = require("./legacy.cjs");
const nativeCommon = require("./native-common.cjs");

export async function runWorkspace(record: LibraryRecord): Promise<string[]> {
  const lazy = await import("./lazy.mjs");
  const computedModule = "./computed.js";
  void import(computedModule);
  void require(computedModule);
  const lengths = ["one", "two"].map((value) => value.length);
  return [
    runtimeCall(),
    viewCall().type,
    lazy.lazyCall(),
    legacy.legacyCall(),
    plainCall(),
    widgetCall().type,
    esmCall(),
    nativeCommon.cjsCall(),
    libraryCall(),
    aliasCall(),
    barrelCall(),
    record.value,
    lengths.join(","),
  ];
}
