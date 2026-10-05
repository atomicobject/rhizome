package embeddings

// MetadataForProvider builds index metadata from a provider and its config.
func MetadataForProvider(provider Provider, cfg ProviderConfig) IndexMetadata {
	return IndexMetadata{
		Provider:   cfg.Provider,
		Model:      cfg.Model,
		Dimensions: provider.Dimensions(),
	}
}

// MetadataError wraps metadata validation failures.
type MetadataError struct {
	Err error
}

func (e MetadataError) Error() string {
	if e.Err == nil {
		return "metadata error"
	}
	return e.Err.Error()
}

func (e MetadataError) Unwrap() error {
	return e.Err
}
