const rows = await fetch("assets/sales.json").then((response) => response.json());
document.querySelector("#total").textContent = `${rows.length} sales rows loaded`;
document.querySelector("#invalid-export").addEventListener("click", () => {
  window.rhizome.download("bad\nname.csv", "text/csv", "invalid").then(
    () => {
      document.body.dataset.invalidExportResult = "accepted";
    },
    () => {
      document.body.dataset.invalidExportResult = "rejected";
    },
  );
});
document.querySelector("#export").addEventListener("click", () => {
  const csv = ["region,amount", ...rows.map((row) => `${row.region},${row.amount}`)].join("\n");
  document.body.dataset.exportResult = "pending";
  window.rhizome.download("sales.csv", "text/csv", csv).then(
    () => {
      document.body.dataset.exportResult = "accepted";
    },
    () => {
      document.body.dataset.exportResult = "rejected";
    },
  );
});
const linkedCSV = ["region,amount", ...rows.map((row) => `${row.region},${row.amount}`)].join("\n");
document.querySelector("#blob-export").href = URL.createObjectURL(
  new Blob([linkedCSV], { type: "text/csv" }),
);
