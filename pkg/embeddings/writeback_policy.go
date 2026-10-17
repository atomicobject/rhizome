package embeddings

import "time"

// WritebackFlushPolicy bounds rows, bytes, and idle time in one write batch.
type WritebackFlushPolicy struct {
	Rows  int
	Bytes int
	Idle  time.Duration
}

// CodeWritebackBatchConfig is shared by semantic producers and their writer.
type CodeWritebackBatchConfig struct {
	QueueCapacity int
	ItemEmbed     WritebackFlushPolicy
	CodeChunks    WritebackFlushPolicy
	IntelEmbed    WritebackFlushPolicy
}
