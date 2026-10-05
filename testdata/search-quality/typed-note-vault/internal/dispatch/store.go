package dispatch

type OperationStore interface {
	Save(Operation) error
}

type MemoryOperationStore struct {
	operations map[string]Operation
}

func (s *MemoryOperationStore) Save(op Operation) error {
	if s.operations == nil {
		s.operations = map[string]Operation{}
	}
	s.operations[op.ID] = op
	return nil
}
