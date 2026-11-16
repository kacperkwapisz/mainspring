// Turns Mainspring's /v1/models rows into pi model definitions.
import type { Config, ServedModel } from "./client.ts";

type Api = "anthropic-messages" | "openai-responses" | "openai-completions";
const APIS: readonly string[] = ["anthropic-messages", "openai-responses", "openai-completions"];

// pi's thinking levels and the provider value each one sends. pi hides xhigh and
// max unless a model maps them, and shows the others unless one maps to null,
// so the map follows the efforts the model really accepts.
const PI_LEVELS = ["minimal", "low", "medium", "high", "xhigh", "max"] as const;

export function thinkingLevelMap(efforts: string[] | undefined): Record<string, string | null> | undefined {
	if (!efforts || !efforts.length) return undefined;
	const map: Record<string, string | null> = {};
	for (const level of PI_LEVELS) map[level] = efforts.includes(level) ? level : null;
	return map;
}

/** The pi API for a model: what Mainspring says, else a guess from the id. */
export function apiFor(m: ServedModel): Api {
	if (m.api && APIS.includes(m.api)) return m.api as Api;
	if (/^claude-/.test(m.id)) return "anthropic-messages";
	if (/^(gpt-|o\d)/.test(m.id)) return "openai-responses";
	return "openai-completions";
}

// A reasoning model with no efforts (Grok -reasoning, Cursor -thinking) thinks on
// its own and Mainspring drops any effort pi sends. Mapping every adjustable level
// to null leaves pi only "off", which sends no effort, and the model keeps its
// reasoning flag. Claude on Messages is left alone: it takes a thinking budget.
function levelsFor(m: ServedModel, api: Api) {
	if (m.efforts && m.efforts.length) return thinkingLevelMap(m.efforts);
	if (m.reasoning && api !== "anthropic-messages") {
		return { minimal: null, low: null, medium: null, high: null } as Record<string, string | null>;
	}
	return undefined;
}

export function costFor(m: ServedModel) {
	const base = {
		input: m.cost?.input ?? 0,
		output: m.cost?.output ?? 0,
		cacheRead: m.cost?.cache_read ?? 0,
		cacheWrite: m.cost?.cache_write ?? 0,
	};
	// A tier replaces the base rate for the whole request once its input is above
	// the threshold. A rate the tier leaves out stays at the base rate.
	const tiers = (m.cost?.tiers ?? [])
		.filter((t) => typeof t.input_tokens_above === "number")
		.map((t) => ({
			inputTokensAbove: t.input_tokens_above,
			input: t.input ?? base.input,
			output: t.output ?? base.output,
			cacheRead: t.cache_read ?? base.cacheRead,
			cacheWrite: t.cache_write ?? base.cacheWrite,
		}));
	return tiers.length ? { ...base, tiers } : base;
}

export function modelConfig(config: Config, m: ServedModel) {
	const api = apiFor(m);
	const adaptive = api === "anthropic-messages" && Boolean(m.efforts && m.efforts.length);
	return {
		id: m.id,
		name: m.name || m.id,
		api,
		// Anthropic's client adds /v1 itself.
		baseUrl: api === "anthropic-messages" ? config.url : `${config.url}/v1`,
		input: (m.input ?? ["text"]).includes("image") ? (["text", "image"] as ("text" | "image")[]) : (["text"] as ("text" | "image")[]),
		reasoning: Boolean(m.reasoning || (m.efforts && m.efforts.length)),
		thinkingLevelMap: levelsFor(m, api),
		// Without this pi sends a thinking budget and never the effort level.
		...(adaptive ? { compat: { forceAdaptiveThinking: true } } : {}),
		// API list prices from Mainspring, so pi shows what the tokens would cost
		// on the API. Your subscriptions pay for them.
		cost: costFor(m),
		// A window that is too large makes pi compact too late.
		contextWindow: m.context_length || 128_000,
		maxTokens: m.max_output_tokens || 32_000,
	};
}
