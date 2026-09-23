package types

// KnowledgeProcessOverrides stores per-upload parse config overrides in knowledge metadata.
type KnowledgeProcessOverrides struct {
	// SummaryEnabled defaults to true when omitted for backward compatibility.
	SummaryEnabled           *bool                     `json:"summary_enabled,omitempty"`
	ParserEngineRules        []ParserEngineRule        `json:"parser_engine_rules,omitempty"`
	ChunkingConfig           *ChunkingConfig           `json:"chunking_config,omitempty"`
	EnableMultimodel         *bool                     `json:"enable_multimodel,omitempty"`
	VLMConfig                *VLMConfig                `json:"vlm_config,omitempty"`
	ASRConfig                *ASRConfig                `json:"asr_config,omitempty"`
	QuestionGenerationConfig *QuestionGenerationConfig `json:"question_generation_config,omitempty"`
	GraphEnabled             *bool                     `json:"graph_enabled,omitempty"`
	ExtractConfig            *ExtractConfig            `json:"extract_config,omitempty"`
	// ParserEngineOverrides passes key-value configuration to docreader parsers
	// (e.g. pdf_force_scanned=true). Merged with workspace-level overrides in the
	// parse pipeline; per-upload values take priority on conflict.
	ParserEngineOverrides map[string]string `json:"parser_engine_overrides,omitempty"`
}

// SystemParseDefaults is the platform-wide parse configuration a SystemAdmin
// publishes via the system_settings key "knowledge.parse_defaults". When
// Enabled is true the embedded overrides replace whatever per-upload or
// per-document overrides were supplied — every ingestion path resolves
// against these values merged over the KB-level defaults, so workspace
// admins can upload without configuring parse settings themselves.
// The JSON shape is {"enabled": bool, ...KnowledgeProcessOverrides} so the
// exact payload the upload dialog emits is reusable verbatim.
type SystemParseDefaults struct {
	Enabled bool `json:"enabled"`
	KnowledgeProcessOverrides
}

// EffectiveProcessConfig is the merged view used by the parse pipeline.
type EffectiveProcessConfig struct {
	SummaryEnabled           bool
	ChunkingConfig           ChunkingConfig
	EnableMultimodel         bool
	VLMConfig                VLMConfig
	ASRConfig                ASRConfig
	QuestionGenerationConfig QuestionGenerationConfig
	GraphEnabled             bool
	ExtractConfig            ExtractConfig
}
