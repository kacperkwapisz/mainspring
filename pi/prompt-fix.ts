// pi's system prompt has three lines that this rewrites before a Claude
// request leaves pi, so Claude models see them in a plainer form. Only the
// system prompt is touched; tool results and messages are sent as they are.
const REWRITES: Array<[string, string]> = [
	[
		"- When asked about: extensions (docs/extensions.md, examples/extensions/), themes (docs/themes.md), skills (docs/skills.md), prompt templates (docs/prompt-templates.md), TUI components (docs/tui.md), keybindings (docs/keybindings.md), SDK integrations (docs/sdk.md), custom providers (docs/custom-provider.md), adding models (docs/models.md), pi packages (docs/packages.md)",
		"- When asked about: extensions, themes, skills, prompt templates, TUI components, keybindings, SDK integrations, custom providers, adding models, and pi packages (docs/extensions.md, examples/extensions/, docs/themes.md, docs/skills.md, docs/prompt-templates.md, docs/tui.md, docs/keybindings.md, docs/sdk.md, docs/custom-provider.md, docs/models.md, docs/packages.md)",
	],
	[
		"- When working on pi topics, read the docs and examples, and follow .md cross-references before implementing",
		"- When working on pi topics, read the docs and examples, and follow cross-references before implementing",
	],
	[
		"- Always read pi .md files completely and follow links to related docs (e.g., tui.md for TUI API details)",
		"- Read pi .md files completely and follow links to related docs (e.g., tui.md for TUI API details)",
	],
];

function rewrite(text: string): string {
	let out = text;
	for (const [from, to] of REWRITES) out = out.split(from).join(to);
	return out;
}

/** Returns the payload with its system prompt rewritten, or undefined when nothing changed. */
export function fixSystemPrompt(payload: any): any {
	const system = payload?.system;
	if (typeof system === "string") {
		const next = rewrite(system);
		return next === system ? undefined : { ...payload, system: next };
	}
	if (!Array.isArray(system)) return undefined;
	let changed = false;
	const next = system.map((block: any) => {
		if (typeof block?.text !== "string") return block;
		const text = rewrite(block.text);
		if (text === block.text) return block;
		changed = true;
		return { ...block, text };
	});
	return changed ? { ...payload, system: next } : undefined;
}
