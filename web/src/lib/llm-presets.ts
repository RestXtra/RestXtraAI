// LLM 供应商预设 + 环境变量配置导入。
// 「供应商预设」概念：一键填充格式/端点/模型/认证头，
// 只需补 API Key 即可连接。含 OpenCode GO 等常用网关。

export type LLMAuthMode = "" | "x-api-key" | "bearer";

export interface LLMPreset {
  name: string;
  description?: string;
  format: "anthropic" | "openai";
  base_url: string;
  model: string;
  auth_mode: LLMAuthMode;
  context_window_k?: number;
  // 预设默认空 key 由用户填写；提供样例便于理解
  api_key_placeholder?: string;
}

export const LLM_PRESETS: LLMPreset[] = [
  {
    name: "OpenCode GO",
    description: "OpenCode 的 Zen GO 网关 · Anthropic 兼容 · deepseek-v4-flash（端点同时接受 x-api-key 与 Bearer）",
    format: "anthropic",
    base_url: "https://opencode.ai/zen/go",
    model: "deepseek-v4-flash",
    auth_mode: "x-api-key",
    api_key_placeholder: "sk-…（OpenCode Zen GO 令牌）",
  },
  {
    name: "Anthropic",
    description: "Claude 官方 API",
    format: "anthropic",
    base_url: "https://api.anthropic.com",
    model: "claude-opus-4-8",
    auth_mode: "x-api-key",
    context_window_k: 200,
  },
  {
    name: "OpenAI",
    description: "OpenAI 官方 API",
    format: "openai",
    base_url: "https://api.openai.com/v1",
    model: "gpt-4o",
    auth_mode: "bearer",
  },
  {
    name: "DeepSeek",
    description: "DeepSeek 官方 API（OpenAI 兼容）",
    format: "openai",
    base_url: "https://api.deepseek.com",
    model: "deepseek-chat",
    auth_mode: "bearer",
  },
  {
    name: "Moonshot Kimi",
    description: "月之暗面 Kimi（OpenAI 兼容）",
    format: "openai",
    base_url: "https://api.moonshot.cn/v1",
    model: "kimi-k2.6",
    auth_mode: "bearer",
  },
  {
    name: "Zhipu GLM",
    description: "智谱 GLM（OpenAI 兼容）",
    format: "openai",
    base_url: "https://open.bigmodel.cn/api/paas/v4",
    model: "glm-5.1",
    auth_mode: "bearer",
  },
  {
    name: "阿里云百炼",
    description: "阿里云百炼（兼容模式，OpenAI 兼容）",
    format: "openai",
    base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1",
    model: "qwen-plus",
    auth_mode: "bearer",
  },
  {
    name: "SiliconFlow",
    description: "硅基流动（OpenAI 兼容）",
    format: "openai",
    base_url: "https://api.siliconflow.cn/v1",
    model: "deepseek-ai/DeepSeek-V3",
    auth_mode: "bearer",
  },
  {
    name: "Ollama（本地）",
    description: "本地 Ollama（OpenAI 兼容，无需 Key）",
    format: "openai",
    base_url: "http://127.0.0.1:11434/v1",
    model: "qwen3:8b",
    auth_mode: "bearer",
  },
];

// 从 env 键值推导的导入结果。
export interface ImportedLLMProfile {
  name: string;
  format: "anthropic" | "openai";
  base_url: string;
  model: string;
  api_key: string;
  auth_mode: LLMAuthMode;
  reasoning_effort?: string;
  context_window_k?: number;
  proxy?: string;
}

// parseEnvJsonProfile 解析供应商配置（{"env":{...}} 或直接 env 对象），
// 兼容 ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN / ANTHROPIC_API_KEY / ANTHROPIC_MODEL
// 与 OPENAI_BASE_URL / OPENAI_API_KEY / OPENAI_MODEL，以及 RESTXTRA_LLM_*。
// 返回错误串（非合法 JSON / 无可识别变量）。
export function parseEnvJsonProfile(text: string): ImportedLLMProfile | string {
  let obj: Record<string, unknown>;
  try {
    obj = JSON.parse(text);
  } catch {
    return '不是合法的 JSON，请粘贴供应商配置（形如 {"env":{...}}）。';
  }
  if (!obj || typeof obj !== "object") {
    return 'JSON 顶层应为对象（形如 {"env":{...}}）。';
  }
  const envRaw = (obj as Record<string, unknown>).env;
  const env: Record<string, unknown> =
    envRaw && typeof envRaw === "object" ? (envRaw as Record<string, unknown>) : (obj as Record<string, unknown>);

  const pick = (...keys: string[]): string => {
    for (const k of keys) {
      const v = env[k];
      if (typeof v === "string" && v.trim() !== "") return v.trim();
    }
    return "";
  };

  const provider = (pick("RESTXTRA_LLM_PROVIDER") || "").toLowerCase();
  const anthBase = pick("ANTHROPIC_BASE_URL");
  const anthModel = pick("ANTHROPIC_MODEL");
  const authToken = pick("ANTHROPIC_AUTH_TOKEN");
  const anthKey = pick("ANTHROPIC_API_KEY");
  const oaiBase = pick("OPENAI_BASE_URL");
  const oaiModel = pick("OPENAI_MODEL");
  const oaiKey = pick("OPENAI_API_KEY");
  const restBase = pick("RESTXTRA_LLM_BASE_URL");
  const restModel = pick("RESTXTRA_LLM_MODEL");

  const hasAnth = Boolean(anthBase || anthModel || authToken || anthKey);
  const hasOai = Boolean(oaiBase || oaiModel || oaiKey);

  let format: "anthropic" | "openai";
  if (provider === "openai") format = "openai";
  else if (provider === "anthropic") format = "anthropic";
  else if (hasOai && !hasAnth) format = "openai";
  else if (hasAnth) format = "anthropic";
  else if (restBase || restModel) format = "anthropic";
  else return "未识别到可用的供应商变量（期望 ANTHROPIC_* 或 OPENAI_* 环境变量）。";

  const base_url = format === "anthropic" ? anthBase || restBase : oaiBase || restBase;
  const model = format === "anthropic" ? anthModel || restModel : oaiModel || restModel;
  const api_key = format === "anthropic" ? authToken || anthKey : oaiKey;

  // ANTHROPIC_AUTH_TOKEN → Authorization: Bearer；其余按各格式默认头。
  let auth_mode: LLMAuthMode;
  if (format === "anthropic") {
    auth_mode = authToken ? "bearer" : "x-api-key";
  } else {
    auth_mode = "bearer";
  }

  if (!base_url) {
    return format === "anthropic"
      ? "缺少 ANTHROPIC_BASE_URL，无法确定 API 端点。"
      : "缺少 OPENAI_BASE_URL，无法确定 API 端点。";
  }
  if (!model) {
    return format === "anthropic" ? "缺少 ANTHROPIC_MODEL，无法确定模型。" : "缺少 OPENAI_MODEL，无法确定模型。";
  }

  let name = typeof obj.name === "string" && obj.name.trim() ? obj.name.trim() : "";
  if (!name) {
    name = /opencode/i.test(base_url) ? "OpenCode GO" : `导入 ${format}`;
  }

  return {
    name,
    format,
    base_url,
    model,
    api_key,
    auth_mode,
    reasoning_effort: pick("RESTXTRA_LLM_REASONING_EFFORT", "REASONING_EFFORT") || undefined,
    proxy: pick("RESTXTRA_LLM_PROXY") || undefined,
  };
}

export const AUTH_MODE_OPTIONS: { value: LLMAuthMode; label: string; hint: string }[] = [
  { value: "x-api-key", label: "x-api-key（默认）", hint: "Anthropic 官方默认认证头，OpenCode GO 也接受" },
  {
    value: "bearer",
    label: "Bearer（ANTHROPIC_AUTH_TOKEN）",
    hint: "Authorization: Bearer，兼容 Claude Code 中继类网关",
  },
];
