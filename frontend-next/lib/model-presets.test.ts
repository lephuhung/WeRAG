// Presets and thinking-control defaults for the model form.
// Runs with: node --experimental-strip-types --test lib/model-presets.test.ts
// (Node >= 22.6, type stripping)
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  MODEL_PRESETS,
  defaultThinkingControl,
  isDeepSeekAlwaysThinking,
  resolveThinkingControl,
} from "./model-presets.ts";

describe("deepseek thinking control", () => {
  it("defaults to thinking.type for the DeepSeek endpoint", () => {
    assert.equal(defaultThinkingControl("deepseek", "https://api.deepseek.com/v1", "deepseek-v4.1-flash"), "thinking_type");
    assert.equal(defaultThinkingControl("generic", "https://api.deepseek.com/v1", "deepseek-v4-pro"), "thinking_type");
  });

  it("leaves always-thinking models alone", () => {
    assert.equal(isDeepSeekAlwaysThinking("deepseek-reasoner"), true);
    assert.equal(isDeepSeekAlwaysThinking("deepseek-r1-distill-70b"), true);
    assert.equal(isDeepSeekAlwaysThinking("deepseek-v4.1-flash"), false);
    assert.equal(defaultThinkingControl("deepseek", "https://api.deepseek.com/v1", "deepseek-reasoner"), "none");
  });

  it("keeps the provider defaults elsewhere", () => {
    assert.equal(defaultThinkingControl("generic", "https://vllm.internal/v1", "qwen3"), "chat_template_kwargs");
    assert.equal(defaultThinkingControl("aliyun", "https://dashscope.aliyuncs.com/v1", "qwen3-max"), "enable_thinking");
    assert.equal(defaultThinkingControl("zhipu", "https://open.bigmodel.cn/api/paas/v4", "glm-4.6"), "thinking_type");
    assert.equal(defaultThinkingControl("openai", "https://api.openai.com/v1", "gpt-4o"), "none");
  });

  it("prefers a stored value over the default", () => {
    assert.equal(resolveThinkingControl("none", "deepseek", "https://api.deepseek.com/v1", "deepseek-v4-pro"), "none");
    assert.equal(resolveThinkingControl("thinking_type", "generic", "https://vllm.internal/v1", "qwen3"), "thinking_type");
    // Unknown or missing values fall back to the provider default.
    assert.equal(resolveThinkingControl("bogus", "aliyun", "https://dashscope.aliyuncs.com/v1"), "enable_thinking");
    assert.equal(resolveThinkingControl(undefined, "generic", "https://vllm.internal/v1"), "chat_template_kwargs");
  });
});

describe("model presets", () => {
  it("has one preset per id and a usable base URL or an explicit custom entry", () => {
    const ids = MODEL_PRESETS.map((p) => p.id);
    assert.equal(new Set(ids).size, ids.length);
    for (const preset of MODEL_PRESETS) {
      assert.ok(preset.label.length > 0);
      assert.ok(preset.provider.length > 0);
      if (preset.id !== "openai-compatible") {
        assert.match(preset.baseUrl, /^https?:\/\//);
      }
    }
  });

  it("fills the thinking control the backend needs for DeepSeek", () => {
    const deepseek = MODEL_PRESETS.find((p) => p.id === "deepseek");
    assert.ok(deepseek);
    const control = defaultThinkingControl(deepseek.provider, deepseek.baseUrl, deepseek.models[0]);
    assert.equal(control, "thinking_type");
  });
});
