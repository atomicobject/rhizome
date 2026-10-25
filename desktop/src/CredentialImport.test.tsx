import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { request, type CredentialStatus } from "./api";
import { CredentialImport } from "./CredentialImport";

vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  request: vi.fn(),
}));
const call = vi.mocked(request);
const status: CredentialStatus = {
  available: true,
  ready: [],
  source: {
    account: "example.1password.com",
    environment: "a".repeat(26),
    keys: ["VOYAGE_API_KEY", "TYPESAFE_API_KEY"],
  },
};
beforeEach(() => vi.resetAllMocks());

it("re-imports only selected keys with non-secret source references and refreshes readiness", async () => {
  call.mockImplementation(async (operation) => {
    if (operation === "credential-status") return status;
    if (operation === "credential-import") return { ...status, ready: ["VOYAGE_API_KEY"] };
    throw new Error("unexpected operation");
  });
  const onImported = vi.fn();
  render(<CredentialImport onImported={onImported} />);
  expect(await screen.findByRole("button", { name: "Re-import selected keys" })).toBeVisible();
  expect(call).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole("checkbox", { name: "TypeSafe" }));
  fireEvent.click(screen.getByRole("button", { name: "Re-import selected keys" }));
  await screen.findByText("Keys available: Voyage.");
  expect(onImported).toHaveBeenCalledOnce();
  expect(call).toHaveBeenCalledWith("credential-import", {
    source: { ...status.source, keys: ["VOYAGE_API_KEY"] },
  });
  expect(document.querySelector('input[type="password"]')).toBeNull();
});

it("shows actionable errors and allows retry", async () => {
  call.mockImplementation(async (operation) => {
    if (operation === "credential-status") return { available: false, ready: [] };
    throw { code: "credential_import", message: "Update to stable 1Password CLI 2.41 or newer." };
  });
  render(<CredentialImport />);
  await screen.findByText(/op command was not found/);
  fireEvent.change(screen.getByLabelText("1Password account"), {
    target: { value: "example.1password.com" },
  });
  fireEvent.change(screen.getByLabelText("Environment ID"), { target: { value: "a".repeat(26) } });
  fireEvent.click(screen.getByRole("button", { name: "Import selected keys" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Update to stable 1Password CLI 2.41 or newer.",
  );
  expect(screen.getByRole("button", { name: "Import selected keys" })).toBeEnabled();
});

it("cancels an in-flight import and refreshes status from disk", async () => {
  let rejectImport: (value: unknown) => void = () => {};
  let reads = 0;
  call.mockImplementation(async (operation) => {
    if (operation === "credential-status") {
      reads += 1;
      return reads === 1 ? status : { ...status, ready: ["TYPESAFE_API_KEY"] };
    }
    if (operation === "credential-import")
      return new Promise<CredentialStatus>((_resolve, reject) => {
        rejectImport = reject;
      });
    if (operation === "credential-cancel") {
      rejectImport(new Error("canceled"));
      return null;
    }
    throw new Error("unexpected operation");
  });
  render(<CredentialImport />);
  fireEvent.click(await screen.findByRole("button", { name: "Re-import selected keys" }));
  fireEvent.click(await screen.findByRole("button", { name: "Cancel import" }));
  await screen.findByText("Import canceled. Checking the saved provider status…");
  await screen.findByText("Keys available: TypeSafe.");
  expect(reads).toBe(2);
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Re-import selected keys" })).toBeEnabled(),
  );
});

it("keeps edited source fields after a failed import and retries that source", async () => {
  call.mockImplementation(async (operation) => {
    if (operation === "credential-status") return status;
    throw new Error("Environment is missing TYPESAFE_API_KEY");
  });
  render(<CredentialImport />);
  await screen.findByRole("button", { name: "Re-import selected keys" });
  fireEvent.change(screen.getByLabelText("1Password account"), {
    target: { value: "new.1password.com" },
  });
  fireEvent.change(screen.getByLabelText("Environment ID"), { target: { value: "b".repeat(26) } });
  fireEvent.click(screen.getByRole("checkbox", { name: "Voyage" }));
  fireEvent.click(screen.getByRole("button", { name: "Re-import selected keys" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Re-import selected keys" })).toBeEnabled(),
  );
  expect(screen.getByLabelText("1Password account")).toHaveValue("new.1password.com");
  expect(screen.getByLabelText("Environment ID")).toHaveValue("b".repeat(26));
  fireEvent.click(screen.getByRole("button", { name: "Re-import selected keys" }));
  expect(call).toHaveBeenLastCalledWith("credential-import", {
    source: {
      account: "new.1password.com",
      environment: "b".repeat(26),
      keys: ["TYPESAFE_API_KEY"],
    },
  });
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Re-import selected keys" })).toBeEnabled(),
  );
});

it("keeps edits made while the initial status is loading", async () => {
  let resolveStatus: (value: CredentialStatus) => void = () => {};
  call.mockImplementation(
    () =>
      new Promise<CredentialStatus>((resolve) => {
        resolveStatus = resolve;
      }),
  );
  render(<CredentialImport />);
  fireEvent.change(screen.getByLabelText("1Password account"), {
    target: { value: "new.1password.com" },
  });
  resolveStatus(status);
  await screen.findByRole("button", { name: "Re-import selected keys" });
  expect(screen.getByLabelText("1Password account")).toHaveValue("new.1password.com");
});
