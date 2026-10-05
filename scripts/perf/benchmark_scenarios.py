"""Scenario declarations for one-shot runtime performance evidence."""

from __future__ import annotations

from benchmark_evidence import ScenarioExecutionContract


FILE_CONTEXT_SCENARIOS = (
    "repeatedIndexedFileContextCode",
    "repeatedIndexedFileContextDirectory",
    "repeatedIndexedFileContextMarkdown",
    "repeatedIndexedFileContextMixed",
)


def default_execution_contract(name: str) -> ScenarioExecutionContract:
    if name in FILE_CONTEXT_SCENARIOS:
        return ScenarioExecutionContract(output_contract="file-context-v1")
    return ScenarioExecutionContract(output_contract="agent-start-v2")


def scenario_commands(
    binary: str,
    directory_target: str,
    file_context_code_target: str = "pkg/app/cli/file_context.go",
    file_context_directory_target: str = "pkg/app/cli",
    file_context_note_target: str = "docs/reference/subsystems/agent-surface.md",
) -> dict[str, list[str]]:
    start = [binary, "agent", "start"]
    file_context = [binary, "agent", "file-context"]
    return {
        "firstBare": [*start, "--timings"],
        "repeatedBare": [*start, "--timings"],
        "directoryTargeted": [
            *start, "--profile", "code", "--file", directory_target, "--timings",
        ],
        "explicitRich": [
            *start, "--profile", "code", "--ontology", "--file", directory_target,
            "--submodule-depth", "1", "--intent", "Test out some rhizome stuff",
            "--timings",
        ],
        "repeatedIndexedFileContextCode": [
            *file_context, "--profile", "code", "--file", file_context_code_target,
        ],
        "repeatedIndexedFileContextDirectory": [
            *file_context, "--profile", "code", "--file", file_context_directory_target,
            "--submodule-depth", "1",
        ],
        "repeatedIndexedFileContextMarkdown": [
            *file_context, "--profile", "vault", "--file", file_context_note_target,
        ],
        "repeatedIndexedFileContextMixed": [
            *file_context, "--profile", "code",
            "--file", file_context_code_target,
            "--file", file_context_directory_target,
            "--file", file_context_note_target,
            "--submodule-depth", "1",
        ],
    }


def representative_scenario_commands(
    binary: str,
    *,
    code_target: str = "pkg/app/bootstrap/live.go",
    symbol: str = "RuntimeRequirements",
    ontology_query: str = 'query { note(path: "CONTEXT.md") { path title } }',
    search_query: str = "runtime capability",
) -> dict[str, list[str]]:
    return {
        "schemaOnly": [binary, "agent", "ontology-query-schema"],
        "exactIndex": [
            binary, "agent", "code-symbol", "--symbol", symbol, "--path", code_target,
        ],
        "liveNote": [
            binary, "agent", "files", "--input", "README.md",
            "--include-content", "false", "--limit", "1",
        ],
        "ontologyQuery": [binary, "agent", "ontology-query", "--query", ontology_query],
        "unavailableIndex": [
            binary, "agent", "file-context", "--profile", "code", "--file", code_target,
        ],
        "rootSearch": [binary, "search", search_query, "--fast", "--limit", "5"],
    }


def representative_execution_contracts() -> dict[str, ScenarioExecutionContract]:
    json_contract = ScenarioExecutionContract(output_contract="json-v1")
    return {
        "schemaOnly": json_contract,
        "exactIndex": json_contract,
        "liveNote": json_contract,
        "ontologyQuery": ScenarioExecutionContract(output_contract="ontology-query-json-v1"),
        "unavailableIndex": ScenarioExecutionContract(
            output_contract="indexed-availability-json-v1"
        ),
        "rootSearch": ScenarioExecutionContract(
            output_format="text",
            json_channel=None,
            output_contract="root-search-text-v1",
        ),
    }
