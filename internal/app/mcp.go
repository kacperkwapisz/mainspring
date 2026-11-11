package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
)

// /mcp is an MCP server (Streamable HTTP, answered as plain JSON) with two
// tools, web_search and web_fetch, that run on the pooled Claude and Codex
// accounts. Apps add it with the same sk-ms- key they use for /v1.

// SettingWebTools turns the MCP web tools on (default) or off.
const SettingWebTools = "mcp.webTools"

var mcpVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func registerMCP(mux *httpx.Mux, hub *proxy.Hub) {
	handle := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			httpx.JSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Mainspring's MCP server answers POST requests"})
			return
		}
		// Browsers send an Origin; only the panel's own origin may call from one.
		if o := r.Header.Get("Origin"); o != "" {
			if u, err := url.Parse(o); err != nil || u.Host != r.Host {
				httpx.JSON(w, http.StatusForbidden, map[string]any{"error": "Cross-site calls to /mcp are refused"})
				return
			}
		}
		key := requireLocalKey(r, hub)
		if key == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mainspring"`)
			httpx.JSON(w, http.StatusUnauthorized, map[string]any{"error": "Send a Mainspring key: Authorization: Bearer sk-ms-…"})
			return
		}
		if !settingOn(hub, SettingWebTools, true) {
			httpx.JSON(w, http.StatusNotFound, map[string]any{"error": "Web tools are turned off in Mainspring's settings"})
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			httpx.JSON(w, 400, rpcFail(nil, -32700, "Couldn't read the request"))
			return
		}
		trimmed := strings.TrimSpace(string(raw))
		if strings.HasPrefix(trimmed, "[") {
			var batch []rpcRequest
			if json.Unmarshal(raw, &batch) != nil {
				httpx.JSON(w, 400, rpcFail(nil, -32700, "That isn't valid JSON-RPC"))
				return
			}
			var out []any
			for _, req := range batch {
				if resp := mcpAnswer(r.Context(), hub, *key, req); resp != nil {
					out = append(out, resp)
				}
			}
			if len(out) == 0 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			httpx.JSON(w, 200, out)
			return
		}
		var req rpcRequest
		if json.Unmarshal(raw, &req) != nil {
			httpx.JSON(w, 400, rpcFail(nil, -32700, "That isn't valid JSON-RPC"))
			return
		}
		resp := mcpAnswer(r.Context(), hub, *key, req)
		if resp == nil { // a notification
			w.WriteHeader(http.StatusAccepted)
			return
		}
		httpx.JSON(w, 200, resp)
	}
	for _, m := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		mux.Handle(m, "/mcp", handle)
	}
}

func rpcOK(id json.RawMessage, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func rpcFail(id json.RawMessage, code int, msg string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": rpcError{Code: code, Message: msg}}
}

func mcpAnswer(ctx context.Context, hub *proxy.Hub, key domain.LocalKey, req rpcRequest) any {
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return nil // notifications get no answer
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := mcpVersions[0]
		for _, v := range mcpVersions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		return rpcOK(req.ID, map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "mainspring", "title": "Mainspring", "version": Version},
			"instructions":    "web_search and web_fetch run on your Mainspring pool's Claude and Codex subscriptions, on the account your current session uses.",
		})
	case "ping":
		return rpcOK(req.ID, map[string]any{})
	case "tools/list":
		return rpcOK(req.ID, map[string]any{"tools": mcpTools()})
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &p) != nil {
			return rpcFail(req.ID, -32602, "tools/call needs a name and arguments")
		}
		return rpcOK(req.ID, callTool(ctx, hub, key, p.Name, p.Arguments))
	default:
		return rpcFail(req.ID, -32601, "Mainspring's MCP server doesn't support "+req.Method)
	}
}

func mcpTools() []any {
	readOnly := map[string]any{"readOnlyHint": true, "openWorldHint": true}
	domains := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	return []any{
		map[string]any{
			"name":        "web_search",
			"title":       "Web search",
			"description": "Search the web and get a short summary with its sources. Runs on your Claude or Codex subscription through Mainspring. Use it for anything current or outside your training data.",
			"inputSchema": map[string]any{
				"type": "object", "required": []string{"query"},
				"properties": map[string]any{
					"query":           map[string]any{"type": "string", "description": "What to search for"},
					"allowed_domains": withDesc(domains, "Only use results from these domains"),
					"blocked_domains": withDesc(domains, "Never use results from these domains"),
				},
			},
			"annotations": readOnly,
		},
		map[string]any{
			"name":        "web_fetch",
			"title":       "Fetch a web page",
			"description": "Read one public web page. With a prompt, a model on your subscription answers the prompt from the page; without one, you get the page's text.",
			"inputSchema": map[string]any{
				"type": "object", "required": []string{"url"},
				"properties": map[string]any{
					"url":    map[string]any{"type": "string", "description": "The full http:// or https:// address"},
					"prompt": map[string]any{"type": "string", "description": "What you need from the page. Leave it out to get the page's text"},
				},
			},
			"annotations": readOnly,
		},
	}
}

func withDesc(schema map[string]any, desc string) map[string]any {
	out := map[string]any{"description": desc}
	for k, v := range schema {
		out[k] = v
	}
	return out
}

func callTool(ctx context.Context, hub *proxy.Hub, key domain.LocalKey, name string, args map[string]any) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	str := func(k string) string { s, _ := args[k].(string); return strings.TrimSpace(s) }
	list := func(k string) []string {
		var out []string
		for _, v := range asList(args[k]) {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	}
	switch name {
	case "web_search":
		q := str("query")
		if q == "" {
			return toolError("Give a query to search for")
		}
		res, err := proxy.WebSearch(ctx, hub, key, q, list("allowed_domains"), list("blocked_domains"))
		if err != nil {
			return toolError("Web search failed: " + err.Error())
		}
		return toolResult(res, "")
	case "web_fetch":
		u := str("url")
		if u == "" {
			return toolError("Give the address of the page")
		}
		title, page, err := proxy.FetchPage(ctx, u, 100_000)
		if err != nil {
			return toolError("Couldn't fetch " + u + ": " + err.Error())
		}
		prompt := str("prompt")
		if prompt == "" {
			head := u
			if title != "" {
				head = title + " (" + u + ")"
			}
			return map[string]any{"content": []any{map[string]any{"type": "text", "text": head + "\n\n" + page}}}
		}
		res, err := proxy.AskAboutPage(ctx, hub, key, u, page, prompt)
		if err != nil {
			return toolError("Couldn't answer from " + u + ": " + err.Error())
		}
		return toolResult(res, u)
	default:
		return toolError("Mainspring has no tool called " + name)
	}
}

func toolResult(res proxy.WebResult, pageURL string) map[string]any {
	var b strings.Builder
	b.WriteString(res.Text)
	if len(res.Sources) > 0 {
		b.WriteString("\n\nSources:")
		for _, s := range res.Sources {
			if s.Title != "" {
				fmt.Fprintf(&b, "\n- %s: %s", s.Title, s.URL)
			} else {
				fmt.Fprintf(&b, "\n- %s", s.URL)
			}
		}
	} else if pageURL != "" {
		b.WriteString("\n\nSource: " + pageURL)
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": b.String()}},
		"structuredContent": map[string]any{"text": res.Text, "sources": res.Sources, "provider": res.Provider, "model": res.Model},
	}
}

func toolError(msg string) map[string]any {
	return map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": msg}}}
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}
