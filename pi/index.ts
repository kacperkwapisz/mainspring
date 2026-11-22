// Mainspring for pi.
//
// - Registers Mainspring as the "mainspring" provider, with the models your
//   subscriptions offer. Each model uses the API Mainspring names for it (Claude
//   on Messages, GPT and Grok on Responses), so caching and sessions behave as
//   they do directly.
// - Adds gpt-image-2 as an image model when the pool has a Codex account, so
//   codemode can generate pictures on your ChatGPT plan.
// - Registers web_search and web_fetch, which run on the subscription account
//   your session already uses.
// - Tells you when Mainspring moves the session to another account.
// - /mainspring shows the pool; /mainspring setup connects to a Mainspring.
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";
import {
	cachedModels,
	callTool,
	envOverrides,
	fetchModels,
	loadConfig,
	savedConfig,
	saveConfig,
	saveModels,
	type Config,
	type ServedModel,
} from "./client.ts";
import { modelConfig } from "./models.ts";
import { IMAGES_API, imageModels, imagesProvider } from "./images.ts";
import { PoolView } from "./pool-view.ts";

const PROVIDER = "mainspring";

// Chat models, plus image models when the pool has a ChatGPT plan.
const allModels = (config: Config, served: ServedModel[]) => [...served.map((m) => modelConfig(config, m)), ...imageModels(config, served)];

function registerProvider(pi: ExtensionAPI, config: Config, models: ServedModel[]) {
	pi.registerProvider(PROVIDER, {
		name: "Mainspring",
		baseUrl: `${config.url}/v1`,
		apiKey: config.key,
		api: "openai-completions",
		models: allModels(config, models) as any,
		images: { [IMAGES_API]: imagesProvider(() => config) },
		// pi asks twice: first without network (startup from the cache), then with it.
		async refreshModels({ allowNetwork, signal }: { allowNetwork: boolean; signal: AbortSignal }) {
			if (!allowNetwork) return allModels(config, cachedModels(config.url)) as any;
			const fresh = await fetchModels(config, signal);
			keepModels(config.url, fresh);
			// An empty answer doesn't wipe a list that worked.
			const served = fresh.length ? fresh : cachedModels(config.url);
			return allModels(config, served) as any;
		},
	} as any);
}

// Saves a list for the next start. Failing to save mustn't stop the list being
// used, so it's reported rather than thrown.
function keepModels(url: string, models: ServedModel[], ctx?: ExtensionContext) {
	try {
		saveModels(url, models);
	} catch (err) {
		const message = `Mainspring: couldn't save the model list: ${(err as Error).message}`;
		if (ctx) tell(ctx, message, "warning");
		else console.error(message);
	}
}

// The headers that key this session on Mainspring, matching its chat requests:
// the extension adds X-Session-ID to every request, and pi's Responses client
// also sends session-id, which Mainspring reads first.
function sessionHeaders(ctx: ExtensionContext): Record<string, string> {
	const id = ctx.sessionManager.getSessionId();
	const headers: Record<string, string> = { "X-Session-ID": `pi-${id}` };
	if (usesMainspring(ctx) && ctx.model?.api === "openai-responses") headers["session-id"] = id;
	return headers;
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
		async execute(_id, params, signal, _onUpdate, ctx) {
			const query = params.query.trim();
			if (!query) throw new Error("A search query is required.");
			if (params.allowed_domains?.length && params.blocked_domains?.length) throw new Error("Use allowed_domains or blocked_domains, not both.");
			const r = await callTool(need(), "web_search", { query, allowed_domains: params.allowed_domains, blocked_domains: params.blocked_domains }, signal, sessionHeaders(ctx));
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
		async execute(_id, params, signal, _onUpdate, ctx) {
			let url = params.url.trim();
			if (!/^https?:\/\//i.test(url)) url = `https://${url}`;
			const r = await callTool(need(), "web_fetch", { url, prompt: params.prompt?.trim() || undefined }, signal, sessionHeaders(ctx));
			return { ...answer(r), details: { url, sources: r.sources, provider: r.provider, model: r.model } };
		},
	});
}

// Without a terminal there's no notification area, so print.
function tell(ctx: ExtensionContext, message: string, level: "info" | "warning" | "error" = "info") {
	if (ctx.hasUI) ctx.ui.notify(message, level);
	else if (level === "info") console.log(message);
	else console.error(message);
}

// Sessions whose account we remember; the oldest go when there are more.
const MAX_REMEMBERED_SESSIONS = 100;

export default async function (pi: ExtensionAPI) {
	let config = loadConfig();
	// Which account served each session last, to notice when it moves.
	const lastAccount = new Map<string, string>();
	let toolsRegistered = false;

	const connect = (c: Config, models: ServedModel[]) => {
		registerProvider(pi, c, models);
		if (!toolsRegistered) {
			registerWebTools(pi, () => config);
			toolsRegistered = true;
		}
	};

	// Start from the cached list, so pi never waits on the network. A fresh list
	// is fetched behind it and replaces the cached one when it differs.
	const start = async (c: Config) => {
		const cached = cachedModels(c.url);
		connect(c, cached);
		const refresh = fetchModels(c, AbortSignal.timeout(cached.length ? 8_000 : 4_000))
			.then(
				(fresh) => {
					// Use the list first; saving it is only for the next start.
					// Skip it if setup switched to another Mainspring meanwhile.
					if (fresh.length && config === c && JSON.stringify(fresh) !== JSON.stringify(cached)) registerProvider(pi, c, fresh);
					keepModels(c.url, fresh);
				},
				() => {
					// Offline: the cached list stands, and pi's own refresh catches up.
				},
			)
			.catch((err) => console.error(`Mainspring: couldn't use the new model list: ${(err as Error).message}`));
		// The very first run has nothing to show, so it waits (briefly) for the list.
		if (!cached.length) await refresh;
	};
	if (config) await start(config);

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
		// Re-insert, so the map stays in least-recently-used order.
		lastAccount.delete(session);
		lastAccount.set(session, account);
		if (lastAccount.size > MAX_REMEMBERED_SESSIONS) lastAccount.delete(lastAccount.keys().next().value as string);
		const from = h["x-mainspring-moved-from"];
		if (from && before && ctx.hasUI) {
			const why = h["x-mainspring-moved-reason"];
			ctx.ui.notify(`Mainspring moved this session from ${from} to ${account}${why ? ` (${why})` : ""}`, "info");
		}
	});

	// A reload keeps the session; every other shutdown ends it.
	pi.on("session_shutdown", (event, ctx) => {
		if (event.reason !== "reload") lastAccount.delete(ctx.sessionManager.getSessionId());
	});

	pi.registerCommand("mainspring", {
		description: "Show your Mainspring pool, or connect: /mainspring setup",
		async handler(args, ctx) {
			const sub = (args ?? "").trim();
			if (sub === "setup" || !config) {
				if (!ctx.hasUI) {
					tell(ctx, "Set MAINSPRING_URL and MAINSPRING_KEY, or run /mainspring setup in pi's terminal.", "warning");
					return;
				}
				// pi's input box has no starting text, so an empty answer keeps the saved value.
				const saved = savedConfig();
				const url = await ctx.ui.input(saved.url ? "Mainspring address (Enter keeps the current one)" : "Mainspring address", saved.url || "http://umbrel.local:8317");
				if (url === undefined) return;
				const address = (url.trim() || saved.url || "").replace(/\/+$/, "").replace(/\/v1$/, "");
				if (!address) return;
				const secret = await ctx.ui.input(saved.key ? "Mainspring key (Enter keeps the current one)" : "Mainspring key (from its Keys page)", "sk-ms-…");
				if (secret === undefined) return;
				const key = secret.trim() || saved.key || "";
				if (!key) return;
				const next = { url: address, key };
				let models: ServedModel[];
				try {
					models = await fetchModels(next, AbortSignal.timeout(8_000));
				} catch (err) {
					tell(ctx, `Couldn't connect: ${(err as Error).message}`, "error");
					return;
				}
				saveConfig(next);
				keepModels(next.url, models, ctx);
				// The environment wins over the file, so the new settings may not be
				// what runs. Then the list has to come from the server that does.
				const overridden = envOverrides();
				const effective = loadConfig() ?? next;
				let served = models;
				if (effective.url !== next.url || effective.key !== next.key) {
					try {
						served = await fetchModels(effective, AbortSignal.timeout(8_000));
						keepModels(effective.url, served, ctx);
					} catch (err) {
						served = [];
						tell(ctx, `Couldn't load models from ${effective.url}: ${(err as Error).message}`, "warning");
					}
				}
				config = effective;
				connect(config, served.length ? served : cachedModels(config.url));
				tell(ctx, "Connected to Mainspring. Pick a model with /model, under Mainspring.", "info");
				if (overridden.length) {
					tell(ctx, `${overridden.join(" and ")} ${overridden.length === 1 ? "is" : "are"} set and win over what you just saved. Unset ${overridden.length === 1 ? "it" : "them"} to use the saved settings.`, "warning");
				}
				return;
			}
			if (ctx.mode !== "tui") {
				tell(ctx, "/mainspring needs pi's terminal.", "warning");
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
