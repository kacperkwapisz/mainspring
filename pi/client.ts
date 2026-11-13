// Talking to Mainspring: settings, the model list, the pool and the MCP tools.
import { existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";

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
}

interface Saved extends Partial<Config> {
	models?: ServedModel[];
}

const FILE = join(homedir(), ".pi", "agent", "mainspring.json");

function readSaved(): Saved {
	try {
		return JSON.parse(readFileSync(FILE, "utf8")) as Saved;
	} catch {
		return {};
	}
}

function writeSaved(next: Saved): void {
	mkdirSync(dirname(FILE), { recursive: true });
	const tmp = `${FILE}.tmp`;
	writeFileSync(tmp, `${JSON.stringify(next, null, 2)}\n`, { mode: 0o600 });
	renameSync(tmp, FILE);
}

/** MAINSPRING_URL and MAINSPRING_KEY win over the saved file. */
export function loadConfig(): Config | undefined {
	const saved = readSaved();
	const url = (process.env.MAINSPRING_URL || saved.url || "").trim().replace(/\/+$/, "").replace(/\/v1$/, "");
	const key = (process.env.MAINSPRING_KEY || saved.key || "").trim();
	return url && key ? { url, key } : undefined;
}

export function saveConfig(config: Config): void {
	writeSaved({ ...readSaved(), url: config.url, key: config.key });
}

export function cachedModels(): ServedModel[] {
	return readSaved().models ?? [];
}

export function saveModels(models: ServedModel[]): void {
	if (existsSync(FILE) || models.length) writeSaved({ ...readSaved(), models });
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
export async function callTool(config: Config, name: string, args: Record<string, unknown>, signal?: AbortSignal): Promise<WebResult> {
	const body = await call<any>(config, "/mcp", {
		method: "POST",
		signal,
		timeoutMs: 150_000,
		headers: { Accept: "application/json, text/event-stream" },
		body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/call", params: { name, arguments: args } }),
	});
	if (body.error) throw new MainspringError(body.error.message ?? "Mainspring's web tools failed");
	const result = body.result ?? {};
	const text = (result.content ?? []).map((c: any) => c.text ?? "").join("\n").trim();
	if (result.isError) throw new MainspringError(text || "Mainspring's web tools failed");
	const sc = result.structuredContent ?? {};
	return { text, sources: sc.sources ?? [], provider: sc.provider, model: sc.model };
}
