// The /mainspring overlay: every account, its limits and when they reset.
import { truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";
import { fetchPool, type Config, type Pool, type PoolAccount, type PoolWindow } from "./client.ts";

type Theme = { fg(color: string, text: string): string; bold(text: string): string };

const STATUS: Record<string, [string, string]> = {
	next: ["success", "next up"],
	active: ["text", "active"],
	cooling: ["warning", "resting"],
	benched: ["muted", "out of rotation"],
	needs_sign_in: ["error", "needs sign-in"],
};

export function inTime(ms: number, now = Date.now()): string {
	const s = Math.max(0, Math.round((ms - now) / 1000));
	if (s < 60) return "<1m";
	const m = Math.round(s / 60);
	if (m < 60) return `${m}m`;
	const h = Math.floor(m / 60);
	if (h < 24) return m % 60 ? `${h}h ${m % 60}m` : `${h}h`;
	const d = Math.floor(h / 24);
	return h % 24 ? `${d}d ${h % 24}h` : `${d}d`;
}

export class PoolView {
	private pool?: Pool;
	private error = "";
	private loading = true;
	private timer: ReturnType<typeof setInterval>;
	private o: { config: Config; theme: Theme; current?: string; requestRender(): void; close(): void };

	constructor(o: { config: Config; theme: Theme; current?: string; requestRender(): void; close(): void }) {
		this.o = o;
		void this.load();
		// Countdowns tick while the overlay is open.
		this.timer = setInterval(() => o.requestRender(), 1000);
	}

	private async load() {
		this.loading = true;
		this.o.requestRender();
		try {
			this.pool = await fetchPool(this.o.config);
			this.error = "";
		} catch (err) {
			this.error = (err as Error).message;
		}
		this.loading = false;
		this.o.requestRender();
	}

	handleInput(data: string) {
		if (data === "\x1b" || data === "q" || data === "\x03") {
			clearInterval(this.timer);
			this.o.close();
		} else if (data === "r") void this.load();
	}

	invalidate() {}

	private bar(w: PoolWindow, width: number, now: number): string {
		const t = this.o.theme;
		const left = w.left ?? 0;
		const used = Math.min(width, Math.round(((100 - left) / 100) * width));
		const cells = Array.from({ length: width }, (_, i) => (i < used ? "█" : "░"));
		// The pace marker: how much of the window has passed.
		if (w.period_ms && w.resets_at) {
			const passed = 1 - (w.resets_at - now) / w.period_ms;
			const at = Math.round(Math.min(1, Math.max(0, passed)) * (width - 1));
			cells[at] = at < used ? "•" : "◦";
		}
		const tone = left <= 10 ? "error" : left <= 25 ? "warning" : "success";
		return t.fg(tone, cells.slice(0, used).join("")) + t.fg("muted", cells.slice(used).join(""));
	}

	private windowLine(w: PoolWindow, now: number, barWidth: number): string {
		const t = this.o.theme;
		const left = w.left == null ? t.fg("dim", "not read yet") : `${Math.round(w.left)}% left`;
		const reset = w.resets_at && w.resets_at > now ? t.fg("dim", ` · resets in ${inTime(w.resets_at, now)}`) : "";
		return `${t.fg("muted", w.label.padEnd(12).slice(0, 12))} ${w.left == null ? "" : this.bar(w, barWidth, now) + " "}${left}${reset}`;
	}

	private account(a: PoolAccount, now: number, width: number): string[] {
		const t = this.o.theme;
		const [tone, word] = STATUS[a.status] ?? ["text", a.status];
		const here = this.o.current && a.label === this.o.current;
		const status =
			a.status === "cooling" && a.cool_until ? `resting · back in ${inTime(a.cool_until, now)}` : word + (a.stale ? " · stale" : "");
		const head = `${here ? t.fg("accent", "●") : " "} ${t.bold(a.label)}${a.plan ? t.fg("dim", ` ${a.plan}`) : ""}`;
		const right = `${t.fg(tone, status)}${a.sessions ? t.fg("dim", ` · ${a.sessions} session${a.sessions === 1 ? "" : "s"}`) : ""}`;
		const gap = Math.max(1, width - visibleWidth(head) - visibleWidth(right));
		const lines = [head + " ".repeat(gap) + right];
		const barWidth = Math.max(8, Math.min(24, width - 40));
		for (const w of a.windows) lines.push("    " + this.windowLine(w, now, barWidth));
		return lines;
	}

	render(width: number): string[] {
		const t = this.o.theme;
		const inner = Math.max(20, width - 4);
		const now = Date.now();
		const out: string[] = [];
		const host = this.o.config.url.replace(/^https?:\/\//, "");
		const keys = t.fg("dim", "r refresh · esc close");
		const title = `${t.bold(t.fg("accent", "Mainspring"))} ${t.fg("dim", host)}`;
		out.push(title + " ".repeat(Math.max(1, inner - visibleWidth(title) - visibleWidth(keys))) + keys, "");
		if (this.error) {
			out.push(t.fg("error", this.error));
		} else if (!this.pool) {
			out.push(t.fg("dim", "Loading…"));
		} else {
			const p = this.pool;
			if (this.o.current) out.push(`${t.fg("muted", "This session")}  ${t.fg("accent", "●")} ${this.o.current}`, "");
			for (const g of p.groups) {
				const next = g.back_at ? t.fg("warning", `every account is out · first back in ${inTime(g.back_at, now)}`) : "";
				const name = t.bold(g.name);
				out.push(name + " ".repeat(Math.max(1, inner - visibleWidth(name) - visibleWidth(next))) + next);
				for (const a of g.accounts) out.push(...this.account(a, now, inner));
				out.push("");
			}
			if (p.others.length) {
				out.push(t.bold("Other sources"));
				for (const o of p.others) {
					const [tone, word] = STATUS[o.status] ?? ["text", o.status];
					out.push(`  ${o.name} ${t.fg("dim", o.label)} ${t.fg(tone, word)}`);
				}
				out.push("");
			}
			if (p.sessions.length) {
				out.push(t.bold("Your live sessions"));
				for (const s of p.sessions) out.push(`  ${s.model} ${t.fg("dim", `on ${s.account || s.provider} · ${s.requests} request${s.requests === 1 ? "" : "s"} · last ${inTime(now, s.last_at)} ago`)}`);
			}
			if (this.loading) out.push(t.fg("dim", "Refreshing…"));
		}
		return out.map((l) => truncateToWidth(`  ${l}`, width));
	}
}
