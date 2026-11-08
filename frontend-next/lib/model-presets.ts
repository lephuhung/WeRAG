/* Presets and thinking-control defaults for the model form.
 *
 * `parameters.extra_config.thinking_control` selects the wire field the
 * backend uses to turn thinking on/off (see internal/models/chat/thinking.go):
 *
 *   none                  send nothing, the provider default applies
 *   chat_template_kwargs  {"chat_template_kwargs":{"enable_thinking":…}}
 *                         vLLM / self-hosted Qwen
 *   enable_thinking       {"enable_thinking":…}
 *                         Alibaba DashScope (Qwen)
 *   thinking_type         {"thinking":{"type":"enabled"|"disabled"}}
 *                         DeepSeek V3.2+/V4, Zhipu GLM, Volcengine
 *
 * The form used to write no thinking_control at all. A DeepSeek model then
 * received chat_template_kwargs (the generic adapter default), which DeepSeek
 * ignores: it stayed in thinking mode and the first agent round — which forces
 * tool_choice — failed with HTTP 400 "Thinking mode does not support this
 * tool_choice". The presets below fill the right value, and the backend also
 * detects the DeepSeek endpoint on its own.
 */

export type ThinkingControl =
  | "none"
  | "chat_template_kwargs"
  | "enable_thinking"
  | "thinking_type";

export const THINKING_CONTROLS: ThinkingControl[] = [
  "none",
  "chat_template_kwargs",
  "enable_thinking",
  "thinking_type",
];

export type ModelPreset = {
  id: string;
  label: string;
  /* Value written to parameters.provider. */
  provider: string;
  /* Value written to parameters.base_url; empty keeps the user's input. */
  baseUrl: string;
  /* Suggested model ids, offered as chips under the name field. */
  models: string[];
};

export const MODEL_PRESETS: ModelPreset[] = [
  {
    id: "deepseek",
    label: "DeepSeek",
    provider: "deepseek",
    baseUrl: "https://api.deepseek.com/v1",
    models: ["deepseek-v4.1-flash", "deepseek-v4-pro", "deepseek-chat", "deepseek-reasoner"],
  },
  {
    id: "openai",
    label: "OpenAI",
    provider: "openai",
    baseUrl: "https://api.openai.com/v1",
    models: ["gpt-5.4", "gpt-5.4-mini", "gpt-4.1", "gpt-4o"],
  },
  {
    id: "qwen",
    label: "Qwen (DashScope)",
    provider: "aliyun",
    baseUrl: "https://dashscope.aliyuncs.com/compatible-mode/v1",
    models: ["qwen3-max", "qwen-plus", "qwen3-235b-a22b"],
  },
  {
    id: "zhipu",
    label: "Zhipu GLM",
    provider: "zhipu",
    baseUrl: "https://open.bigmodel.cn/api/paas/v4",
    models: ["glm-4.6", "glm-4.5"],
  },
  {
    id: "moonshot",
    label: "Moonshot Kimi",
    provider: "generic",
    baseUrl: "https://api.moonshot.cn/v1",
    models: ["kimi-k2-0905-preview", "moonshot-v1-128k"],
  },
  {
    id: "openai-compatible",
    label: "OpenAI-compatible (vLLM, gateway)",
    provider: "generic",
    baseUrl: "",
    models: [],
  },
];

/** Models whose reasoning cannot be switched off over the API. */
export function isDeepSeekAlwaysThinking(modelName: string): boolean {
  const name = modelName.trim().toLowerCase();
  return name.includes("deepseek-r1") || name.includes("deepseek-reasoner");
}

function isDeepSeekEndpoint(provider: string, baseUrl: string): boolean {
  return provider.trim().toLowerCase() === "deepseek"
    || baseUrl.trim().toLowerCase().includes("api.deepseek.com");
}

/**
 * Default thinking control for a provider + endpoint + model.
 * Mirrors the backend adapter defaults so an untouched form saves the same
 * behavior the model already had.
 */
export function defaultThinkingControl(
  provider: string,
  baseUrl = "",
  modelName = "",
): ThinkingControl {
  const p = provider.trim().toLowerCase();

  if (isDeepSeekEndpoint(p, baseUrl)) {
    return isDeepSeekAlwaysThinking(modelName) ? "none" : "thinking_type";
  }

  switch (p) {
    case "generic":
      return "chat_template_kwargs";
    case "aliyun":
      return "enable_thinking";
    case "volcengine":
    case "lkeap":
    case "zhipu":
      return "thinking_type";
    default:
      return "none";
  }
}

/** Stored extra_config value, or the provider default when unset/unknown. */
export function resolveThinkingControl(
  saved: string | undefined,
  provider: string,
  baseUrl = "",
  modelName = "",
): ThinkingControl {
  const value = saved?.trim().toLowerCase();
  if (THINKING_CONTROLS.includes(value as ThinkingControl)) {
    return value as ThinkingControl;
  }
  return defaultThinkingControl(provider, baseUrl, modelName);
}
