// Image models on your ChatGPT plan, through Mainspring's Images API.
import { generateImage, type Config, type ImageAnswer, type ServedModel } from "./client.ts";

export const IMAGES_API = "mainspring-images";

// The model Codex's own image tool asks for. ChatGPT picks the image model
// itself whatever a request names, so there's nothing else to offer. Codex's
// model list doesn't name image models, so it's offered whenever Mainspring
// serves a Codex model.
const IMAGE_MODELS = [{ id: "gpt-image-2", name: "GPT Image 2" }];

/** pi image models for this Mainspring, or none when it has no Codex account. */
export function imageModels(config: Config, served: ServedModel[]) {
	if (!served.some((m) => m.owned_by === "codex")) return [];
	return IMAGE_MODELS.map((m) => ({
		type: "image" as const,
		id: m.id,
		name: m.name,
		api: IMAGES_API,
		baseUrl: `${config.url}/v1`,
		input: ["text", "image"] as ("text" | "image")[],
		output: ["image"] as ("text" | "image")[],
		// Your ChatGPT plan pays for the pictures.
		cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
	}));
}

type Block = { type: "text"; text: string } | { type: "image"; data: string; mimeType: string };

const MIME: Record<string, string> = { png: "image/png", jpeg: "image/jpeg", jpg: "image/jpeg", webp: "image/webp", gif: "image/gif" };

// The picture's type from its first bytes, else from output_format. ChatGPT's
// answer doesn't always name the format.
function mimeOf(b64: string, format?: string): string {
	const head = Buffer.from(b64.slice(0, 24), "base64");
	if (head[0] === 0x89 && head[1] === 0x50) return "image/png";
	if (head[0] === 0xff && head[1] === 0xd8) return "image/jpeg";
	if (head.subarray(0, 3).toString("latin1") === "GIF") return "image/gif";
	if (head.subarray(0, 4).toString("latin1") === "RIFF" && head.subarray(8, 12).toString("latin1") === "WEBP") return "image/webp";
	return MIME[format ?? ""] ?? "image/png";
}

/**
 * pi's generateImages for Mainspring: text blocks become the prompt, and image
 * blocks make it an edit of those pictures.
 */
export function imagesProvider(getConfig: () => Config | undefined) {
	return {
		async generateImages(model: any, context: { input: Block[] }, options?: { signal?: AbortSignal; headers?: Record<string, string> }) {
			const result: any = { api: model.api, provider: model.provider, model: model.id, output: [], stopReason: "stop", timestamp: Date.now() };
			try {
				const config = getConfig();
				if (!config) throw new Error("Mainspring isn't set up. Run /mainspring setup.");
				const prompt = context.input
					.filter((b): b is Extract<Block, { type: "text" }> => b.type === "text")
					.map((b) => b.text)
					.join("\n\n")
					.trim();
				if (!prompt) throw new Error("Image generation needs a text prompt.");
				const images = context.input
					.filter((b): b is Extract<Block, { type: "image" }> => b.type === "image")
					.map((b) => ({ image_url: `data:${b.mimeType};base64,${b.data}` }));
				const body: Record<string, unknown> = { model: model.id, prompt };
				if (images.length) body.images = images;
				const answer: ImageAnswer = await generateImage(config, body, images.length > 0, options?.signal, options?.headers);
				for (const d of answer.data ?? []) {
					if (d.b64_json) result.output.push({ type: "image", data: d.b64_json, mimeType: mimeOf(d.b64_json, answer.output_format) });
				}
				if (!result.output.length) throw new Error("Mainspring returned no image.");
				const u = answer.usage;
				if (u) {
					const input = u.input_tokens ?? 0;
					const output = u.output_tokens ?? 0;
					result.usage = { input, output, cacheRead: 0, cacheWrite: 0, totalTokens: input + output, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } };
				}
			} catch (err) {
				result.stopReason = options?.signal?.aborted ? "aborted" : "error";
				result.errorMessage = (err as Error).message;
			}
			return result;
		},
	};
}
