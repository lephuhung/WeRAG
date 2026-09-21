import type {
  KnowledgeBaseRow,
  KnowledgeProcessOverrides,
  ParserEngineRule,
} from "@/lib/api/knowledge";

/* Ported from frontend/src/views/knowledge/components/UploadConfirmDialog.vue —
 * the UploadUIState + initFromKbInfo/applyOverridesToState/buildProcessOverrides
 * logic, kept free of UI so both the file-upload and reparse flows share it. */

export const IMAGE_EXTENSIONS = ["jpg", "jpeg", "png", "gif", "bmp", "webp"];
export const AUDIO_EXTENSIONS = ["mp3", "wav", "m4a", "flac", "ogg"];

export interface ChunkingUIState {
  chunkSize: number;
  chunkOverlap: number;
  separators: string[];
  parserEngineRules?: ParserEngineRule[];
  enableParentChild: boolean;
  parentChunkSize: number;
  childChunkSize: number;
  strategy: string;
  tokenLimit: number;
  languages: string[];
  tableMetadataInstructions: string;
}

export interface ParseUIState {
  summaryEnabled: boolean;
  chunking: ChunkingUIState;
  multimodal: {
    enabled: boolean;
    vllmModelId: string;
    descriptionLanguage: string;
    customInstructions: string;
  };
  asr: { enabled: boolean; modelId: string; language: string };
  question: { enabled: boolean; questionCount: number; customInstructions: string };
  /* KB-level graph settings are passed through unchanged — the Vue dialog's
   * node/relation editor (GraphSettings.vue) is not ported yet, so the graph
   * section is not editable here. */
  graphEnabled: boolean;
  extractConfig?: KnowledgeProcessOverrides["extract_config"];
  pdfForceScanned: boolean;
}

const DEFAULT_SEPARATORS = ["\n\n", "\n", "。", "！", "？", ";", "；"];

type KbChunking = {
  chunk_size?: number;
  chunk_overlap?: number;
  separators?: string[];
  parser_engine_rules?: ParserEngineRule[];
  enable_parent_child?: boolean;
  parent_chunk_size?: number;
  child_chunk_size?: number;
  strategy?: string;
  token_limit?: number;
  languages?: string[];
  table_metadata_instructions?: string;
};

export function createDefaultUIState(): ParseUIState {
  return {
    summaryEnabled: true,
    chunking: {
      chunkSize: 512,
      chunkOverlap: 80,
      separators: [...DEFAULT_SEPARATORS],
      parserEngineRules: undefined,
      enableParentChild: true,
      parentChunkSize: 4096,
      childChunkSize: 384,
      strategy: "auto",
      tokenLimit: 0,
      languages: [],
      tableMetadataInstructions: "",
    },
    multimodal: { enabled: false, vllmModelId: "", descriptionLanguage: "", customInstructions: "" },
    asr: { enabled: false, modelId: "", language: "" },
    question: { enabled: true, questionCount: 3, customInstructions: "" },
    graphEnabled: false,
    extractConfig: undefined,
    pdfForceScanned: false,
  };
}

/* The KB row's process config fields arrive loosely typed; normalize through
 * these helpers so a missing/extra field can't crash the dialog. */
export function initFromKb(kb: KnowledgeBaseRow | null): ParseUIState {
  const s = createDefaultUIState();
  if (!kb) return s;

  const cc = (kb.chunking_config ?? undefined) as KbChunking | undefined;
  if (cc) {
    s.chunking = {
      chunkSize: cc.chunk_size || 512,
      chunkOverlap: cc.chunk_overlap || 80,
      separators: cc.separators ?? [...DEFAULT_SEPARATORS],
      parserEngineRules: cc.parser_engine_rules ?? undefined,
      enableParentChild: cc.enable_parent_child ?? false,
      parentChunkSize: cc.parent_chunk_size || 4096,
      childChunkSize: cc.child_chunk_size || 384,
      strategy: cc.strategy || "auto",
      tokenLimit: cc.token_limit || 0,
      languages: cc.languages ?? [],
      tableMetadataInstructions: cc.table_metadata_instructions ?? "",
    };
  }

  const vlm = kb.vlm_config;
  if (vlm) {
    s.multimodal = {
      enabled: !!vlm.enabled,
      vllmModelId: vlm.model_id || "",
      descriptionLanguage: vlm.description_language || "",
      customInstructions: vlm.custom_instructions || "",
    };
  }

  const asr = kb.asr_config;
  if (asr) {
    s.asr = {
      enabled: !!asr.enabled,
      modelId: asr.model_id || "",
      language: asr.language || "",
    };
  }

  const qg = kb.question_generation_config;
  if (qg) {
    s.question = {
      enabled: qg.enabled ?? true,
      questionCount: qg.question_count || 3,
      customInstructions: qg.custom_instructions || "",
    };
  }

  const extract = kb.extract_config as KnowledgeProcessOverrides["extract_config"];
  s.extractConfig = extract;
  s.graphEnabled = !!kb.indexing_strategy?.graph_enabled && !!extract?.enabled;
  return s;
}

/* Prefill from the overrides this document was last parsed with
 * (metadata.process_overrides on the knowledge detail response). */
export function applyOverrides(
  state: ParseUIState,
  o?: KnowledgeProcessOverrides | null,
): ParseUIState {
  if (!o) return state;
  const s: ParseUIState = {
    ...state,
    chunking: { ...state.chunking },
    multimodal: { ...state.multimodal },
    asr: { ...state.asr },
    question: { ...state.question },
  };
  if (o.summary_enabled != null) s.summaryEnabled = o.summary_enabled;
  const cc = o.chunking_config;
  if (cc) {
    if (cc.chunk_size != null) s.chunking.chunkSize = cc.chunk_size;
    if (cc.chunk_overlap != null) s.chunking.chunkOverlap = cc.chunk_overlap;
    if (cc.separators) s.chunking.separators = cc.separators;
    if (cc.enable_parent_child != null) s.chunking.enableParentChild = cc.enable_parent_child;
    if (cc.parent_chunk_size != null) s.chunking.parentChunkSize = cc.parent_chunk_size;
    if (cc.child_chunk_size != null) s.chunking.childChunkSize = cc.child_chunk_size;
    if (cc.strategy != null) s.chunking.strategy = cc.strategy;
    if (cc.token_limit != null) s.chunking.tokenLimit = cc.token_limit;
    if (cc.languages) s.chunking.languages = cc.languages;
    if (cc.table_metadata_instructions != null)
      s.chunking.tableMetadataInstructions = cc.table_metadata_instructions;
    if (cc.parser_engine_rules) s.chunking.parserEngineRules = cc.parser_engine_rules;
  }
  if (o.parser_engine_rules) s.chunking.parserEngineRules = o.parser_engine_rules;
  if (o.enable_multimodel != null) s.multimodal.enabled = o.enable_multimodel;
  if (o.vlm_config) {
    if (o.vlm_config.enabled != null) s.multimodal.enabled = o.vlm_config.enabled;
    if (o.vlm_config.model_id != null) s.multimodal.vllmModelId = o.vlm_config.model_id;
    if (o.vlm_config.description_language != null)
      s.multimodal.descriptionLanguage = o.vlm_config.description_language;
    if (o.vlm_config.custom_instructions != null)
      s.multimodal.customInstructions = o.vlm_config.custom_instructions;
  }
  if (o.asr_config) {
    if (o.asr_config.enabled != null) s.asr.enabled = o.asr_config.enabled;
    if (o.asr_config.model_id != null) s.asr.modelId = o.asr_config.model_id;
    if (o.asr_config.language != null) s.asr.language = o.asr_config.language;
  }
  const qg = o.question_generation_config;
  if (qg) {
    if (qg.enabled != null) s.question.enabled = qg.enabled;
    if (qg.question_count != null) s.question.questionCount = qg.question_count;
    if (qg.custom_instructions != null) s.question.customInstructions = qg.custom_instructions;
  }
  if (o.extract_config) s.extractConfig = o.extract_config;
  if (o.graph_enabled != null) s.graphEnabled = o.graph_enabled;
  if (s.extractConfig?.enabled != null) s.graphEnabled = s.graphEnabled && !!s.extractConfig.enabled;
  s.pdfForceScanned = o.parser_engine_overrides?.pdf_force_scanned === "true";
  return s;
}

export function buildProcessOverrides(s: ParseUIState): KnowledgeProcessOverrides {
  const c = s.chunking;
  const overrides: KnowledgeProcessOverrides = {
    summary_enabled: s.summaryEnabled,
    parser_engine_rules: c.parserEngineRules,
    chunking_config: {
      chunk_size: c.chunkSize,
      chunk_overlap: c.chunkOverlap,
      separators: c.separators,
      enable_parent_child: c.enableParentChild,
      parent_chunk_size: c.parentChunkSize,
      child_chunk_size: c.childChunkSize,
      strategy: c.strategy,
      token_limit: c.tokenLimit,
      languages: c.languages,
      table_metadata_instructions: c.tableMetadataInstructions,
    },
    enable_multimodel: s.multimodal.enabled,
    vlm_config: {
      enabled: s.multimodal.enabled,
      model_id: s.multimodal.vllmModelId,
      description_language: s.multimodal.descriptionLanguage,
      custom_instructions: s.multimodal.customInstructions,
    },
    asr_config: {
      enabled: s.asr.enabled,
      model_id: s.asr.modelId,
      language: s.asr.language,
    },
    question_generation_config: {
      enabled: s.question.enabled,
      question_count: s.question.questionCount,
      custom_instructions: s.question.customInstructions,
    },
    graph_enabled: s.graphEnabled,
    extract_config: s.extractConfig,
  };
  if (s.pdfForceScanned) {
    overrides.parser_engine_overrides = { pdf_force_scanned: "true" };
  }
  return overrides;
}

export function fileExt(name: string): string {
  const dot = name.lastIndexOf(".");
  return dot < 0 ? "" : name.substring(dot + 1).toLowerCase();
}
