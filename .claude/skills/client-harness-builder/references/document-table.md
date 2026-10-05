# Template: Document Table Command

Starting template for `.claude/commands/document-table.md`. Replace all `[PLACEHOLDER]` values with codebase-specific content before placing the file in `client-harness/.claude/commands/`.

---

Describe the schema, fields, and usage patterns for a database table in this codebase.

## Usage

Provide the table name you want to document. This command will:

1. Find all usages of the table across the codebase
2. Document each field: data type, nullable status, business meaning, and usage patterns
3. Identify implicit foreign key relationships (columns that reference other tables without a formal FK constraint)
4. Note naming inconsistencies or ambiguities that warrant team clarification

## Context to load

Before starting, load these documentation files:

- @[PLACEHOLDER: path/to/schema-docs/]
- @[PLACEHOLDER: path/to/relevant-module-docs/]
- @CLAUDE.md

## Steps

1. Search for all files that reference `[TABLE_NAME]` (replace with the actual table name provided)
2. For each column, record:
   - Column name and data type (from schema files, migrations, or ORM model)
   - Nullable: yes / no
   - Business meaning: what this field represents in the real world, not just the technical type
   - Where it is set (which code paths write to it) and where it is read (key consumers)
3. Identify columns that appear to be foreign keys without explicit FK constraints — document the implied relationship and which table they reference
4. Flag any fields where meaning is unclear or naming is inconsistent with the pattern elsewhere in the schema; mark them `[REQUIRES CLARIFICATION: <brief description>]`
5. Output as a markdown section following the format in the existing schema docs at @[PLACEHOLDER: path/to/example-table-doc.md]

## Output format

Use this structure for each documented table:

```markdown
## [table_name]

[One-sentence description of what this table stores and which part of the business it supports.]

### Fields

| Column | Type | Nullable | Business Meaning |
|---|---|---|---|
| id | INT | No | Primary key |
| [column] | [type] | [yes/no] | [meaning] |

### Key usage patterns

- **Set by**: [list the main code paths that write to this table]
- **Read by**: [list the main consumers]

### Implicit relationships

- `[column]` references `[other_table].id` — [no FK constraint; inferred from usage in X]

### Ambiguities requiring clarification

- `[REQUIRES CLARIFICATION: field X appears to have two different meanings in modules A and B]`
```

## Known ambiguities

[PLACEHOLDER: List any fields in this table already flagged as ambiguous during the assessment — helps focus clarification on what's already uncertain rather than re-discovering it.]
