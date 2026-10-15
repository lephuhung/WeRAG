package types

// AuxiliaryModelSettingKey names the small model for low-risk side tasks —
// session titles and follow-up suggestions — so the main chat model is left
// to answering. Empty turns it off (those tasks use the answering model).
const AuxiliaryModelSettingKey = "llm.auxiliary_model_id"

// DefaultAuxiliaryModelID is Gemma-4-E4B base, served with the spell-check
// LoRA (config/builtin_models.yaml).
const DefaultAuxiliaryModelID = "builtin-llm-gemma4-e4b"

// CompactionModelSettingKey names the model that writes context-compaction
// summaries (agent history and mid-turn compaction). Empty means the
// answering model; a model that fails falls back to the answering model.
const CompactionModelSettingKey = "llm.compaction_model_id"
