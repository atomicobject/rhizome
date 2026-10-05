package ontology

// SetFieldValueWithWitness stages a field value together with the exact field
// value observed by the caller. Replay may rebase unrelated source drift, but
// it conflicts if the field changes again after this witness was captured.
func (s *EditSession) SetFieldValueWithWitness(ref NodeRef, field string, values, expectedValues []string, expectedValueKind string, list, links, unset bool) error {
	return s.stage(setFieldOp{
		Ref: ref, Field: field, Values: append([]string(nil), values...),
		RequireScalarList: list && !links, IsLinks: links, Unset: unset,
		ExpectedValues: append([]string(nil), expectedValues...), ExpectedValueKind: expectedValueKind, ExpectedCaptured: true,
	})
}
