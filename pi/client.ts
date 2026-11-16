// Talking to Mainspring: settings, the model list, the pool and the MCP tools.
import { mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { getAgentDir } from "@earendil-works/pi-coding-agent";

export interface Config {
	url: string; // the panel's address, without /v1
	key: string; // sk-ms-…
}

export interface ServedModel {
	id: string;
	name?: string;
	context_length?: number;
	max_output_tokens?: number;
	input?: string[];
	reasoning?: boolean;
	efforts?: string[];
	// The pi API that keeps the model's features: anthropic-messages,
	// openai-responses or openai-completions. Older Mainsprings leave it out.
	api?: string;
	// API list prices in USD per million tokens, when Mainspring knows them.
	cost?: {
		input?: number;
		output?: number;
		cache_read?: number;
		cache_write?: number;
		// Rates that replace the base ones once a request's input passes a size.
		tiers?: { input_tokens_above: number; input?: number; output?: number; cache_read?: number; cache_write?: number }[];
	};
}

interface Saved extends Partial<Config> {
	/** The last model list each Mainspring served, by its address. */
	modelsByUrl?: Record<string, ServedModel[]>;
	/**
	 * The list for `url`. Released extensions before modelsByUrl read only this,
	 * so it's kept in step with modelsByUrl[url] for anyone who goes back.
	 */
	models?: ServedModel[];
}

// In pi's agent directory (~/.pi/agent unless PI_CODING_AGENT_DIR moves it).
const file = () => join(getAgentDir(), "mainspring.json");

function readSaved(): Saved {
	let saved: Saved;
	try {
		saved = JSON.parse(readFileSync(file(), "utf8")) as Saved;
	} catch {
		return {};
	}
	// A file from an older extension has only the top-level list, which belongs
	// to its saved address. File it there unless that address has its own.
	const url = saved.url ? normalizeUrl(saved.url) : "";
	if (url && saved.models?.length && !saved.modelsByUrl?.[url]) saved.modelsByUrl = { ...saved.modelsByUrl, [url]: saved.models };
	return saved;
}

function writeSaved(next: Saved): void {
	// The top-level list always belongs to the saved address, or isn't there.
	const current = next.url ? next.modelsByUrl?.[normalizeUrl(next.url)] : undefined;
	const out: Saved = { ...next, models: current?.length ? current : undefined };
	mkdirSync(dirname(file()), { recursive: true });
	const tmp = `${file()}.tmp`;
	writeFileSync(tmp, `${JSON.stringify(out, null, 2)}\n`, { mode: 0o600 });
	renameSync(tmp, file());
}

/** The panel's address as the extension keys it: no trailing slash or /v1. */
export function normalizeUrl(url: string): string {
	return url.trim().replace(/\/+$/, "").replace(/\/v1$/, "");
}

/** MAINSPRING_URL and MAINSPRING_KEY win over the saved file. */
export function loadConfig(): Config | undefined {
	const saved = readSaved();
	const url = normalizeUrl(process.env.MAINSPRING_URL || saved.url || "");
	const key = (process.env.MAINSPRING_KEY || saved.key || "").trim();
	return url && key ? { url, key } : undefined;
}

/** The settings that MAINSPRING_URL / MAINSPRING_KEY override, by variable name. */
export function envOverrides(): string[] {
	return ["MAINSPRING_URL", "MAINSPRING_KEY"].filter((name) => (process.env[name] || "").trim());
}

export function saveConfig(config: Config): void {
	writeSaved({ ...readSaved(), url: config.url, key: config.key });
}

/** The saved settings, ignoring the environment, so setup can offer them as the current ones. */
export function savedConfig(): Partial<Config> {
	const { url, key } = readSaved();
	return { url, key };
}

/** The last list this Mainspring served, so another instance's models never show up. */
export function cachedModels(url: string): ServedModel[] {
	return readSaved().modelsByUrl?.[url] ?? [];
}

/** Keeps a list for this address. An empty list never replaces a real one. */
export function saveModels(url: string, models: ServedModel[]): void {
	if (!models.length) return;
	const saved = readSaved();
	writeSaved({ ...saved, modelsByUrl: { ...saved.modelsByUrl, [url]: models } });
}

export class MainspringError extends Error {}

async function call<T>(config: Config, path: string, init: RequestInit & { timeoutMs?: number } = {}): Promise<T> {
	const timeout = AbortSignal.timeout(init.timeoutMs ?? 15_000);
	const signal = init.signal ? AbortSignal.any([init.signal, timeout]) : timeout;
	let res: Response;
	try {
		res = await fetch(config.url + path, {
			...init,
			signal,
			headers: { Authorization: `Bearer ${config.key}`, "Content-Type": "application/json", ...(init.headers ?? {}) },
		});
	} catch (err) {
		if (init.signal?.aborted) throw new MainspringError("Cancelled.");
		throw new MainspringError(`Couldn't reach Mainspring at ${config.url}: ${(err as Error).message}`);
	}
	const text = await res.text();
	let body: any;
	try {
		body = text ? JSON.parse(text) : {};
	} catch {
		body = { error: { message: text.slice(0, 200) } };
	}
	if (!res.ok) {
		const msg = typeof body?.error === "string" ? body.error : body?.error?.message;
		if (res.status === 401) throw new MainspringError("Mainspring refused the key. Run /mainspring setup with a key from its Keys page.");
		throw new MainspringError(msg || `Mainspring answered ${res.status}`);
	}
	return body as T;
}

export async function fetchModels(config: Config, signal?: AbortSignal): Promise<ServedModel[]> {
	const body = await call<{ data: ServedModel[] }>(config, "/v1/models", { signal, timeoutMs: 8_000 });
	return body.data ?? [];
}

export interface PoolWindow {
	label: string;
	left: number | null;
	resets_at?: number;
	period_ms?: number;
}
export interface PoolAccount {
	label: string;
	plan?: string;
	status: string;
	stale?: boolean;
	cool_until?: number;
	sessions: number;
	windows: PoolWindow[];
}
export interface Pool {
	now: number;
	groups: { provider: string; name: string; next_up: string; back_at?: number; accounts: PoolAccount[] }[];
	others: { provider: string; name: string; label: string; status: string }[];
	sessions: { provider: string; account: string; model: string; client: string; requests: number; last_at: number }[];
	/** Open incidents on providers' status pages (Mainspring 1.7.0 and later). */
	incidents?: { provider: string; company: string; name: string; status: string; started_at: number; url: string }[];
}

export function fetchPool(config: Config): Promise<Pool> {
	return call<Pool>(config, "/v1/pool");
}

export interface WebResult {
	text: string;
	sources: { title?: string; url: string }[];
	provider?: string;
	model?: string;
}

/** Calls one of Mainspring's MCP tools (stateless JSON-RPC over HTTP). */
export async function callTool(config: Config, name: string, args: Record<string, unknown>, signal?: AbortSignal, sessionHeaders: Record<string, string> = {}): Promise<WebResult> {
	const body = await call<any>(config, "/mcp", {
		method: "POST",
		signal,
		timeoutMs: 150_000,
		// The same session headers chat requests send, so the tool runs on this session's account.
		headers: { Accept: "application/json, text/event-stream", ...sessionHeaders },
		body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/call", params: { name, arguments: args } }),
	});
	if (body.error) throw new MainspringError(body.error.message ?? "Mainspring's web tools failed");
	const result = body.result ?? {};
	const text = (result.content ?? []).map((c: any) => c.text ?? "").join("\n").trim();
	if (result.isError) throw new MainspringError(text || "Mainspring's web tools failed");
	const sc = result.structuredContent ?? {};
	return { text, sources: sc.sources ?? [], provider: sc.provider, model: sc.model };
}
