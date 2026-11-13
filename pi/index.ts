// Mainspring for pi.
//
// - Registers Mainspring as the "mainspring" provider, with the models your
//   subscriptions offer. Claude models speak the Anthropic API and GPT models
//   the Responses API, so caching and sessions behave as they do directly.
// - Registers web_search and web_fetch, which run on the subscription account
//   your session already uses.
// - Tells you when Mainspring moves the session to another account.
// - /mainspring shows the pool; /mainspring setup connects to a Mainspring.
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";
import { cachedModels, callTool, fetchModels, loadConfig, saveConfig, saveModels, type Config, type ServedModel } from "./client.ts";
import { PoolView } from "./pool-view.ts";

const PROVIDER = "mainspring";

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

function modelConfig(config: Config, m: ServedModel) {
	const id = m.id;
	const claude = /^claude-/.test(id);
	const responses = /^(gpt-|o\d)/.test(id);
	return {
		id,
		name: m.name || id,
		api: claude ? "anthropic-messages" : responses ? "openai-responses" : "openai-completions",
		// Anthropic's client adds /v1 itself.
		baseUrl: claude ? config.url : `${config.url}/v1`,
		input: (m.input ?? ["text"]).includes("image") ? (["text", "image"] as ("text" | "image")[]) : (["text"] as ("text" | "image")[]),
		reasoning: Boolean(m.reasoning || (m.efforts && m.efforts.length)),
		thinkingLevelMap: thinkingLevelMap(m.efforts),
		// Your subscriptions pay for these; pi shows no per-token cost.
		cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
		contextWindow: m.context_length || 200_000,
		maxTokens: m.max_output_tokens || 32_000,
	};
}

function registerProvider(pi: ExtensionAPI, config: Config, models: ServedModel[]) {
	pi.registerProvider(PROVIDER, {
		name: "Mainspring",
		baseUrl: `${config.url}/v1`,
		apiKey: config.key,
		api: "openai-completions",
		models: models.map((m) => modelConfig(config, m)) as any,
		async refreshModels({ signal }: { signal: AbortSignal }) {
			const fresh = await fetchModels(config, signal);
			saveModels(fresh);
			return fresh.map((m) => modelConfig(config, m)) as any;
		},
	} as any);
}

function usesMainspring(ctx: ExtensionContext): boolean {
	return ctx.model?.provider === PROVIDER;
}

function registerWebTools(pi: ExtensionAPI, getConfig: () => Config | undefined) {
	const need = () => {
		const c = getConfig();
		if (!c) throw new Error("Mainspring isn't set up. Run /mainspring setup.");
		return c;
	};
	const answer = (r: { text: string }) => ({ content: [{ type: "text" as const, text: r.text }] });
	pi.registerTool({
		name: "web_search",
		label: "Web search",
		description:
			"Search the public web and get back a short written answer with its sources. Runs on your Claude or Codex subscription through Mainspring. Use it for anything current or outside your training data: library versions, changelogs, API docs, error messages, release dates.",
		promptSnippet: "Search the web through Mainspring; returns a short answer with its sources",
		promptGuidelines: [
			"Use web_search for current facts (versions, docs, changelogs, errors); write the query as a specific question, and name the library and version when they matter.",
			'Use allowed_domains to keep a web_search to official sources (e.g. ["docs.python.org"]) when the answer should come from them.',
		],
		parameters: Type.Object({
			query: Type.String({ description: 'What to find out, as a specific question (e.g. "Vite 7 breaking changes for library mode").' }),
			allowed_domains: Type.Optional(Type.Array(Type.String(), { description: "Only use results from these domains. Not with blocked_domains." })),
			blocked_domains: Type.Optional(Type.Array(Type.String(), { description: "Never use results from these domains. Not with allowed_domains." })),
		}),
		async execute(_id, params, signal) {
			const query = params.query.trim();
			if (!query) throw new Error("A search query is required.");
			if (params.allowed_domains?.length && params.blocked_domains?.length) throw new Error("Use allowed_domains or blocked_domains, not both.");
			const r = await callTool(need(), "web_search", { query, allowed_domains: params.allowed_domains, blocked_domains: params.blocked_domains }, signal);
			return { ...answer(r), details: { query, sources: r.sources, provider: r.provider, model: r.model } };
		},
	});
	pi.registerTool({
		name: "web_fetch",
		label: "Web fetch",
		description:
			"Read one public web page. With a prompt, a model on your subscription answers it from the page through Mainspring; without one, you get the page's text.",
		promptSnippet: "Read a web page through Mainspring and answer a question about it",
		promptGuidelines: ["Use web_fetch when you have a URL and need what the page says; give a precise prompt rather than asking for the whole page."],
		parameters: Type.Object({
			url: Type.String({ description: "The page to read, e.g. https://vitejs.dev/guide/migration" }),
			prompt: Type.Optional(Type.String({ description: "What you need from the page. Leave it out to get the page's text." })),
		}),
		async execute(_id, params, signal) {
			let url = params.url.trim();
			if (!/^https?:\/\//i.test(url)) url = `https://${url}`;
			const r = await callTool(need(), "web_fetch", { url, prompt: params.prompt?.trim() || undefined }, signal);
			return { ...answer(r), details: { url, sources: r.sources, provider: r.provider, model: r.model } };
		},
	});
}

export default async function (pi: ExtensionAPI) {
	let config = loadConfig();
	// Which account served this session last, to notice when it moves.
	const lastAccount = new Map<string, string>();
	let toolsRegistered = false;

	const connect = async (c: Config) => {
		let models = cachedModels();
		try {
			models = await fetchModels(c, AbortSignal.timeout(4_000));
			saveModels(models);
		} catch {
			// Offline at startup: use the last list; refreshModels catches up.
		}
		registerProvider(pi, c, models);
		if (!toolsRegistered) {
			registerWebTools(pi, () => config);
			toolsRegistered = true;
		}
	};
	if (config) await connect(config);

	// Keep a pi session on one Mainspring account, and let web tools follow it.
	pi.on("before_provider_headers", (event, ctx) => {
		if (!usesMainspring(ctx)) return;
		event.headers["X-Session-ID"] = `pi-${ctx.sessionManager.getSessionId()}`;
	});

	pi.on("after_provider_response", (event, ctx) => {
		if (!usesMainspring(ctx) || event.status >= 400) return;
		const h = Object.fromEntries(Object.entries(event.headers).map(([k, v]) => [k.toLowerCase(), v]));
		const account = h["x-mainspring-account"];
		if (!account) return;
		const session = ctx.sessionManager.getSessionId();
		const before = lastAccount.get(session);
		lastAccount.set(session, account);
		const from = h["x-mainspring-moved-from"];
		if (from && before && ctx.hasUI) {
			const why = h["x-mainspring-moved-reason"];
			ctx.ui.notify(`Mainspring moved this session to ${account}${why ? `: ${from}, ${why}` : ""}`, "info");
		}
	});

	pi.registerCommand("mainspring", {
		description: "Show your Mainspring pool, or connect: /mainspring setup",
		async handler(args, ctx) {
			const sub = (args ?? "").trim();
			if (sub === "setup" || !config) {
				if (!ctx.hasUI) {
					ctx.ui.notify("Set MAINSPRING_URL and MAINSPRING_KEY, or run /mainspring setup in pi's terminal.", "warning");
					return;
				}
				const url = (await ctx.ui.input("Mainspring address", config?.url || "http://umbrel.local:8317"))?.trim();
				if (!url) return;
				const key = (await ctx.ui.input("Mainspring key (from its Keys page)", "sk-ms-…"))?.trim();
				if (!key) return;
				const next = { url: url.replace(/\/+$/, "").replace(/\/v1$/, ""), key };
				try {
					await fetchModels(next, AbortSignal.timeout(8_000));
				} catch (err) {
					ctx.ui.notify(`Couldn't connect: ${(err as Error).message}`, "error");
					return;
				}
				saveConfig(next);
				config = next;
				await connect(next);
				ctx.ui.notify("Connected to Mainspring. Pick a model with /model, under Mainspring.", "info");
				return;
			}
			if (ctx.mode !== "tui") {
				ctx.ui.notify("/mainspring needs pi's terminal.", "warning");
				return;
			}
			const c = config;
			const session = ctx.sessionManager.getSessionId();
			await ctx.ui.custom<void>(
				(tui, theme, _kb, done) => new PoolView({ config: c, theme, current: usesMainspring(ctx) ? lastAccount.get(session) : undefined, requestRender: () => tui.requestRender(), close: () => done(undefined) }),
				{ overlay: true, overlayOptions: { anchor: "center", width: Math.min(100, Math.max(60, Math.floor((process.stdout.columns || 100) * 0.9))), maxHeight: "85%", margin: 1 } },
			);
		},
	});
}
