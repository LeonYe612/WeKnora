package types

import (
	"database/sql/driver"
	"encoding/json"
	"sync/atomic"
)

// IndexingStrategy controls which indexing pipelines are active for a knowledge base.
// Each boolean flag independently enables/disables a processing pipeline.
// When a document is uploaded, only the enabled pipelines will run.
type IndexingStrategy struct {
	// VectorEnabled enables semantic vector embedding and search
	VectorEnabled bool `yaml:"vector_enabled" json:"vector_enabled" extensions:"x-agent"`
	// KeywordEnabled enables keyword-based (BM25) search
	KeywordEnabled bool `yaml:"keyword_enabled" json:"keyword_enabled" extensions:"x-agent"`
	// WikiEnabled enables automatic wiki page generation from documents
	WikiEnabled bool `yaml:"wiki_enabled" json:"wiki_enabled" extensions:"x-agent"`
	// GraphEnabled enables knowledge graph entity/relation extraction
	GraphEnabled bool `yaml:"graph_enabled" json:"graph_enabled" extensions:"x-agent"`
}

// defaultGraphEnabled 决定新建知识库是否默认开启知识图谱。
// 默认 false，与上游行为完全一致；仅当部署侧显式设置 DEFAULT_GRAPH_ENABLED=true
// 时由 SetDefaultGraphEnabled 打开。做成运行时开关而不是改死默认值，是为了
// 让本部署的定制与上游保持零语义分叉（merge 时不会产生冲突，也随时可回退）。
var defaultGraphEnabled atomic.Bool

// SetDefaultGraphEnabled 由启动流程依据 DEFAULT_GRAPH_ENABLED 调用。
func SetDefaultGraphEnabled(v bool) { defaultGraphEnabled.Store(v) }

// DefaultIndexingStrategy returns the default strategy matching the legacy behavior:
// vector and keyword indexing enabled, wiki and graph disabled.
func DefaultIndexingStrategy() IndexingStrategy {
	return IndexingStrategy{
		VectorEnabled:  true,
		KeywordEnabled: true,
		WikiEnabled:    false,
		GraphEnabled:   defaultGraphEnabled.Load(),
	}
}

// NeedsEmbedding returns true if any pipeline that requires an embedding model is enabled.
func (s IndexingStrategy) NeedsEmbedding() bool {
	return s.VectorEnabled || s.KeywordEnabled
}

// NeedsChunks returns true if any pipeline that requires document chunks is enabled.
// Chunks are needed for vector indexing, keyword indexing, wiki generation, and graph extraction.
func (s IndexingStrategy) NeedsChunks() bool {
	return s.VectorEnabled || s.KeywordEnabled || s.WikiEnabled || s.GraphEnabled
}

// HasAnyIndexing returns true if at least one indexing pipeline is enabled.
func (s IndexingStrategy) HasAnyIndexing() bool {
	return s.VectorEnabled || s.KeywordEnabled || s.WikiEnabled || s.GraphEnabled
}

// IsZero returns true if the strategy has no pipelines enabled (zero value).
func (s IndexingStrategy) IsZero() bool {
	return !s.VectorEnabled && !s.KeywordEnabled && !s.WikiEnabled && !s.GraphEnabled
}

// Value implements the driver.Valuer interface for GORM serialization.
func (s IndexingStrategy) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// Scan implements the sql.Scanner interface for GORM deserialization.
// When the database column is NULL (existing rows before migration),
// it returns DefaultIndexingStrategy() for backward compatibility.
func (s *IndexingStrategy) Scan(value interface{}) error {
	if value == nil {
		*s = DefaultIndexingStrategy()
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		*s = DefaultIndexingStrategy()
		return nil
	}
	if err := json.Unmarshal(b, s); err != nil {
		*s = DefaultIndexingStrategy()
		return nil
	}
	return nil
}
