# Deliberately invalid source and trace fixture

This negative fixture is expected to fail validation. It contains an
unsupported `RequirementSource.source-kind`, broken source/spec wikilinks, and
story and acceptance-criterion references that do not resolve. Report those
diagnostics and preserve the explicit invalid state; do not silently repair or
promote the note.
