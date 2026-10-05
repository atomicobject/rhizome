const React = require("react");
const { useMemo: memo } = require("react");
const createClient = require("@acme/client").default;
const WidgetLibrary = require("@acme/widgets/react");
const { readFileSync } = require("node:fs");
require("source-map-support/register");

function exerciseExternalCommonJS(unknownClient, moduleName) {
  const client = createClient();
  const widget = new WidgetLibrary.Widget();
  const element = React.createElement("span", null, memo(() => "ready", []));

  client.render(element);
  unknownClient.refresh(); // Unknown receiver members must remain unresolved.
  require(moduleName); // Computed module sources must remain unresolved.

  return readFileSync(widget.path, "utf8");
}

module.exports = { exerciseExternalCommonJS };
