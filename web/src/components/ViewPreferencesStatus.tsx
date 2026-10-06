import { useViewPreferences } from "../viewPreferences/hooks";
import type { ViewPreferenceScope } from "../viewPreferences/types";

export function ViewPreferencesStatus({
  scope,
  vaultKey,
  includeWidgets = false,
  validationError,
}: {
  scope: ViewPreferenceScope | null;
  vaultKey: string | null | undefined;
  includeWidgets?: boolean;
  validationError?: Error | null;
}) {
  const preferences = useViewPreferences(scope, vaultKey ?? null);

  if (!preferences.store) return null;
  const error = preferences.error ?? validationError;

  return (
    <>
      {error && (
        <span className="view-preferences view-preferences--error" role="alert">
          {preferences.pending
            ? "Your view preferences are not saved."
            : "Some view preferences could not load."}
          <button type="button" onClick={() => void preferences.retry().catch(() => {})}>
            Retry
          </button>
        </span>
      )}
      {!error && preferences.pending && (
        <span className="view-preferences view-preferences--pending" role="status">
          Saving preferences…
        </span>
      )}
      {(includeWidgets || Object.keys(preferences.values).length > 0) && (
        <button
          type="button"
          className="view-preferences__reset"
          title="Forget your personal settings for this view and use its shared configuration"
          onClick={() =>
            void (includeWidgets ? preferences.resetFamily() : preferences.resetAll()).catch(
              () => {},
            )
          }
        >
          Reset to shared
        </button>
      )}
    </>
  );
}
