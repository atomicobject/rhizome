import { ApiError } from "../api/publicClient";
import type { ViewErrorInfo } from "../components/ConfiguredView";

export function queryErrorMessage(error: Error) {
  return error.message || "The request failed.";
}

export function viewErrorFromUnknown(error: Error): ViewErrorInfo {
  return error instanceof ApiError
    ? { message: error.message, status: error.status, code: error.code, details: error.details }
    : { message: queryErrorMessage(error) };
}
