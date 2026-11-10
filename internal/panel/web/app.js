// Mainspring panel. Plain Alpine, no build step.
(() => {
  const ROUTES = ["pool", "sessions", "keys", "meter", "sources", "settings"];
  const DAY = 86400000;

  // ---------- formatting ----------
  const pad = (n) => String(n).padStart(2, "0");
  const fmt = {
    clock(ms) {
      if (!ms) return "";
      const d = new Date(ms), now = new Date();
      const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
      if (d.toDateString() === now.toDateString()) return hm;
      return `${d.toLocaleDateString("en-GB", { weekday: "short" })} ${hm}`;
    },
    day(ms) {
      if (!ms) return "";
      return new Date(ms).toLocaleDateString("en-GB", { weekday: "short", day: "numeric", month: "short" });
    },
    in(ms, now = Date.now()) {
      if (!ms) return "";
      const s = Math.max(0, Math.round((ms - now) / 1000));
      if (s < 60) return "in under a min";
      const m = Math.round(s / 60);
      if (m < 60) return `in ${m} min`;
      const h = Math.floor(m / 60), rm = m % 60;
      if (h < 6) return rm ? `in ${h} h ${rm} min` : `in ${h} h`;
      if (h < 24) return `in ${Math.round(m / 60)} h`;
      const d = Math.floor(h / 24), rh = Math.round(h % 24);
      if (d < 3 && rh) return `in ${d} d ${rh} h`;
      return `in ${Math.round(h / 24)} d`;
    },
    ago(ms, now = Date.now()) {
      if (!ms) return "never";
      const s = Math.max(0, Math.round((now - ms) / 1000));
      if (s < 60) return `${s} s ago`;
      const m = Math.round(s / 60);
      if (m < 60) return `${m} min ago`;
      const h = Math.round(m / 60);
      if (h < 24) return `${h} h ago`;
      return `${Math.round(h / 24)} d ago`;
    },
    tokens(n) {
      n = Number(n) || 0;
      if (n >= 1e9) return (n / 1e9).toFixed(1) + "B";
      if (n >= 1e6) return (n / 1e6).toFixed(1) + "M";
      if (n >= 1e3) return (n / 1e3).toFixed(n >= 1e4 ? 0 : 1) + "k";
      return String(n);
    },
    usd: (n) => "$" + (Number(n) || 0).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 }),
    int: (n) => (Number(n) || 0).toLocaleString("en-US"),
    pct: (v) => (v == null ? "—" : Math.round(v) + "%"),
    plural: (n, one, many) => `${fmt.int(n)} ${n === 1 ? one : many}`,
  };

  // ---------- icons ----------
  // Interface icons: Lucide (ISC), 24px grid. Provider marks: LobeHub icons (MIT), fill-based.
  const ICON = {
    "pool": "<path d=\"M12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83z\"/> <path d=\"M2 12a1 1 0 0 0 .58.91l8.6 3.91a2 2 0 0 0 1.65 0l8.58-3.9A1 1 0 0 0 22 12\"/> <path d=\"M2 17a1 1 0 0 0 .58.91l8.6 3.91a2 2 0 0 0 1.65 0l8.58-3.9A1 1 0 0 0 22 17\"/>",
    "sessions": "<path d=\"M22 12h-2.48a2 2 0 0 0-1.93 1.46l-2.35 8.36a.25.25 0 0 1-.48 0L9.24 2.18a.25.25 0 0 0-.48 0l-2.35 8.36A2 2 0 0 1 4.49 12H2\"/>",
    "keys": "<path d=\"M2.586 17.414A2 2 0 0 0 2 18.828V21a1 1 0 0 0 1 1h3a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h1a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h.172a2 2 0 0 0 1.414-.586l.814-.814a6.5 6.5 0 1 0-4-4z\"/> <circle cx=\"16.5\" cy=\"7.5\" r=\".5\" fill=\"currentColor\"/>",
    "meter": "<path d=\"M3 3v16a2 2 0 0 0 2 2h16\"/> <path d=\"M18 17V9\"/> <path d=\"M13 17V5\"/> <path d=\"M8 17v-3\"/>",
    "sources": "<path d=\"M12 22v-5\"/> <path d=\"M15 8V2\"/> <path d=\"M17 8a1 1 0 0 1 1 1v4a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V9a1 1 0 0 1 1-1z\"/> <path d=\"M9 8V2\"/>",
    "settings": "<path d=\"M10 5H3\"/> <path d=\"M12 19H3\"/> <path d=\"M14 3v4\"/> <path d=\"M16 17v4\"/> <path d=\"M21 12h-9\"/> <path d=\"M21 19h-5\"/> <path d=\"M21 5h-7\"/> <path d=\"M8 10v4\"/> <path d=\"M8 12H3\"/>",
    "lock": "<rect width=\"18\" height=\"11\" x=\"3\" y=\"11\" rx=\"2\" ry=\"2\"/> <path d=\"M7 11V7a5 5 0 0 1 10 0v4\"/>",
    "copy": "<rect width=\"14\" height=\"14\" x=\"8\" y=\"8\" rx=\"2\" ry=\"2\"/> <path d=\"M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2\"/>",
    "plus": "<path d=\"M5 12h14\"/> <path d=\"M12 5v14\"/>",
    "close": "<path d=\"M18 6 6 18\"/> <path d=\"m6 6 12 12\"/>",
    "back": "<path d=\"m15 18-6-6 6-6\"/>",
    "chevron": "<path d=\"m9 18 6-6-6-6\"/>",
    "more": "<circle cx=\"12\" cy=\"12\" r=\"1\"/> <circle cx=\"19\" cy=\"12\" r=\"1\"/> <circle cx=\"5\" cy=\"12\" r=\"1\"/>",
    "key": "<circle cx=\"10\" cy=\"7\" r=\"1\"/> <circle cx=\"4\" cy=\"20\" r=\"1\"/> <path d=\"M4.7 19.3 19 5\"/> <path d=\"m21 3-3 1 2 2Z\"/> <path d=\"M9.26 7.68 5 12l2 5\"/> <path d=\"m10 14 5 2 3.5-3.5\"/> <path d=\"m18 12 1-1 1 1-1 1Z\"/>",
    "passkey": "<path d=\"M12 10a2 2 0 0 0-2 2c0 1.02-.1 2.51-.26 4\"/> <path d=\"M14 13.12c0 2.38 0 6.38-1 8.88\"/> <path d=\"M17.29 21.02c.12-.6.43-2.3.5-3.02\"/> <path d=\"M2 12a10 10 0 0 1 18-6\"/> <path d=\"M2 16h.01\"/> <path d=\"M21.8 16c.2-2 .131-5.354 0-6\"/> <path d=\"M5 19.5C5.5 18 6 15 6 12a6 6 0 0 1 .34-2\"/> <path d=\"M8.65 22c.21-.66.45-1.32.57-2\"/> <path d=\"M9 6.8a6 6 0 0 1 9 5.2v2\"/>",
    "check": "<path d=\"M20 6 9 17l-5-5\"/>",
    "info": "<circle cx=\"12\" cy=\"12\" r=\"10\"/> <path d=\"M12 16v-4\"/> <path d=\"M12 8h.01\"/>",
    "alert": "<circle cx=\"12\" cy=\"12\" r=\"10\"/> <line x1=\"12\" x2=\"12\" y1=\"8\" y2=\"12\"/> <line x1=\"12\" x2=\"12.01\" y1=\"16\" y2=\"16\"/>",
    "clock": "<circle cx=\"12\" cy=\"12\" r=\"10\"/> <path d=\"M12 6v6l4 2\"/>",
    "external": "<path d=\"M15 3h6v6\"/> <path d=\"M10 14 21 3\"/> <path d=\"M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6\"/>",
    "openai": "<rect width=\"20\" height=\"8\" x=\"2\" y=\"2\" rx=\"2\" ry=\"2\"/> <rect width=\"20\" height=\"8\" x=\"2\" y=\"14\" rx=\"2\" ry=\"2\"/> <line x1=\"6\" x2=\"6.01\" y1=\"6\" y2=\"6\"/> <line x1=\"6\" x2=\"6.01\" y1=\"18\" y2=\"18\"/>",
  };
  const BRAND = {
    "claude": "<path d=\"M4.709 15.955l4.72-2.647.08-.23-.08-.128H9.2l-.79-.048-2.698-.073-2.339-.097-2.266-.122-.571-.121L0 11.784l.055-.352.48-.321.686.06 1.52.103 2.278.158 1.652.097 2.449.255h.389l.055-.157-.134-.098-.103-.097-2.358-1.596-2.552-1.688-1.336-.972-.724-.491-.364-.462-.158-1.008.656-.722.881.06.225.061.893.686 1.908 1.476 2.491 1.833.365.304.145-.103.019-.073-.164-.274-1.355-2.446-1.446-2.49-.644-1.032-.17-.619a2.97 2.97 0 01-.104-.729L6.283.134 6.696 0l.996.134.42.364.62 1.414 1.002 2.229 1.555 3.03.456.898.243.832.091.255h.158V9.01l.128-1.706.237-2.095.23-2.695.08-.76.376-.91.747-.492.584.28.48.685-.067.444-.286 1.851-.559 2.903-.364 1.942h.212l.243-.242.985-1.306 1.652-2.064.73-.82.85-.904.547-.431h1.033l.76 1.129-.34 1.166-1.064 1.347-.881 1.142-1.264 1.7-.79 1.36.073.11.188-.02 2.856-.606 1.543-.28 1.841-.315.833.388.091.395-.328.807-1.969.486-2.309.462-3.439.813-.042.03.049.061 1.549.146.662.036h1.622l3.02.225.79.522.474.638-.079.485-1.215.62-1.64-.389-3.829-.91-1.312-.329h-.182v.11l1.093 1.068 2.006 1.81 2.509 2.33.127.578-.322.455-.34-.049-2.205-1.657-.851-.747-1.926-1.62h-.128v.17l.444.649 2.345 3.521.122 1.08-.17.353-.608.213-.668-.122-1.374-1.925-1.415-2.167-1.143-1.943-.14.08-.674 7.254-.316.37-.729.28-.607-.461-.322-.747.322-1.476.389-1.924.315-1.53.286-1.9.17-.632-.012-.042-.14.018-1.434 1.967-2.18 2.945-1.726 1.845-.414.164-.717-.37.067-.662.401-.589 2.388-3.036 1.44-1.882.93-1.086-.006-.158h-.055L4.132 18.56l-1.13.146-.487-.456.061-.746.231-.243 1.908-1.312-.006.006z\"></path>",
    "codex": "<path clip-rule=\"evenodd\" d=\"M8.086.457a6.105 6.105 0 013.046-.415c1.333.153 2.521.72 3.564 1.7a.117.117 0 00.107.029c1.408-.346 2.762-.224 4.061.366l.063.03.154.076c1.357.703 2.33 1.77 2.918 3.198.278.679.418 1.388.421 2.126a5.655 5.655 0 01-.18 1.631.167.167 0 00.04.155 5.982 5.982 0 011.578 2.891c.385 1.901-.01 3.615-1.183 5.14l-.182.22a6.063 6.063 0 01-2.934 1.851.162.162 0 00-.108.102c-.255.736-.511 1.364-.987 1.992-1.199 1.582-2.962 2.462-4.948 2.451-1.583-.008-2.986-.587-4.21-1.736a.145.145 0 00-.14-.032c-.518.167-1.04.191-1.604.185a5.924 5.924 0 01-2.595-.622 6.058 6.058 0 01-2.146-1.781c-.203-.269-.404-.522-.551-.821a7.74 7.74 0 01-.495-1.283 6.11 6.11 0 01-.017-3.064.166.166 0 00.008-.074.115.115 0 00-.037-.064 5.958 5.958 0 01-1.38-2.202 5.196 5.196 0 01-.333-1.589 6.915 6.915 0 01.188-2.132c.45-1.484 1.309-2.648 2.577-3.493.282-.188.55-.334.802-.438.286-.12.573-.22.861-.304a.129.129 0 00.087-.087A6.016 6.016 0 015.635 2.31C6.315 1.464 7.132.846 8.086.457zm-.804 7.85a.848.848 0 00-1.473.842l1.694 2.965-1.688 2.848a.849.849 0 001.46.864l1.94-3.272a.849.849 0 00.007-.854l-1.94-3.393zm5.446 6.24a.849.849 0 000 1.695h4.848a.849.849 0 000-1.696h-4.848z\"></path>",
    "grok": "<path d=\"M9.27 15.29l7.978-5.897c.391-.29.95-.177 1.137.272.98 2.369.542 5.215-1.41 7.169-1.951 1.954-4.667 2.382-7.149 1.406l-2.711 1.257c3.889 2.661 8.611 2.003 11.562-.953 2.341-2.344 3.066-5.539 2.388-8.42l.006.007c-.983-4.232.242-5.924 2.75-9.383.06-.082.12-.164.179-.248l-3.301 3.305v-.01L9.267 15.292M7.623 16.723c-2.792-2.67-2.31-6.801.071-9.184 1.761-1.763 4.647-2.483 7.166-1.425l2.705-1.25a7.808 7.808 0 00-1.829-1A8.975 8.975 0 005.984 5.83c-2.533 2.536-3.33 6.436-1.962 9.764 1.022 2.487-.653 4.246-2.34 6.022-.599.63-1.199 1.259-1.682 1.925l7.62-6.815\"></path>",
    "cursor": "<path d=\"M22.106 5.68L12.5.135a.998.998 0 00-.998 0L1.893 5.68a.84.84 0 00-.419.726v11.186c0 .3.16.577.42.727l9.607 5.547a.999.999 0 00.998 0l9.608-5.547a.84.84 0 00.42-.727V6.407a.84.84 0 00-.42-.726zm-.603 1.176L12.228 22.92c-.063.108-.228.064-.228-.061V12.34a.59.59 0 00-.295-.51l-9.11-5.26c-.107-.062-.063-.228.062-.228h18.55c.264 0 .428.286.296.514z\"></path>",
    "opencode": "<path d=\"M16 6H8v12h8V6zm4 16H4V2h16v20z\"></path>",
  };
  // 16px with a 1.2px stroke: 1.8 on Lucide's 24-unit grid.
  const icon = (name, color = "currentColor", size = 16) => {
    if (BRAND[name]) return `<svg width="${size}" height="${size}" viewBox="0 0 24 24" fill="${color}" fill-rule="evenodd" aria-hidden="true">${BRAND[name]}</svg>`;
    return `<svg width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="${color}" stroke-width="${(1.2 * 24 / size).toFixed(2)}" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ICON[name] || ""}</svg>`;
  };
  const MARK = {
    next: '<svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="6.5" fill="rgb(59 155 255 / 28%)"/><circle cx="8" cy="8" r="3.5" fill="#3B9BFF"/></svg>',
    nextNeutral: '<svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="3.5" fill="#E5E5E5"/></svg>',
    active: '<svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="3.5" fill="#4ADE80"/></svg>',
    cooling: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="#A1A1A1" stroke-width="1.2" stroke-linecap="round" aria-hidden="true"><circle cx="8" cy="8" r="5.5"/><path d="M8 5v3l2 1.5"/></svg>',
    benched: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="#737373" stroke-width="1.2" stroke-linecap="round" aria-hidden="true"><circle cx="8" cy="8" r="5.5"/><path d="M6.6 6v4M9.4 6v4"/></svg>',
    needs: '<svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="6.5" fill="rgb(251 191 36 / 22%)"/><circle cx="8" cy="8" r="3.5" fill="#FBBF24"/></svg>',
    stale: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="#737373" stroke-width="1.2" stroke-dasharray="2.2 2.2" aria-hidden="true"><circle cx="8" cy="8" r="5"/></svg>',
  };
  const SPINNER = '<svg class="spin" width="14" height="14" viewBox="0 0 16 16" fill="none" aria-hidden="true"><circle cx="8" cy="8" r="6" stroke="#363636" stroke-width="1.6"/><path d="M8 2a6 6 0 0 1 6 6" stroke="#E5E5E5" stroke-width="1.6" stroke-linecap="round"/></svg>';

  // ---------- HTTP ----------
  class APIError extends Error {
    constructor(status, code, message) { super(message || code || "Request failed"); this.status = status; this.code = code; }
  }
  async function api(method, path, body) {
    const opts = { method, headers: { Accept: "application/json" }, credentials: "same-origin" };
    if (body !== undefined) { opts.headers["Content-Type"] = "application/json"; opts.body = JSON.stringify(body); }
    let res;
    try { res = await fetch(path, opts); } catch { throw new APIError(0, "offline", "Mainspring didn't answer. It may be restarting"); }
    let data = null;
    try { data = await res.json(); } catch { /* empty */ }
    if (!res.ok) {
      const e = (data && data.error) || {};
      if (res.status === 401 && e.code === "panel_signed_out") window.dispatchEvent(new CustomEvent("ms:signed-out"));
      throw new APIError(res.status, typeof e === "string" ? e : e.code, typeof e === "string" ? e : e.message);
    }
    return data;
  }

  // ---------- toasts (Ding, loaded as a module) ----------
  const T = {
    get t() { return window.dingToast; },
    ok(title, description) { this.t ? this.t.success(title, { description }) : console.info(title); },
    err(title, description, retry) {
      if (!this.t) return console.error(title, description);
      this.t.error(title, { description, action: retry ? { label: "Retry", onClick: () => { retry(); } } : undefined });
    },
    busy(title, description) { return this.t ? this.t.loading(title, { description }) : null; },
    done(id, type, title, description) { if (this.t && id != null) this.t.update(id, { type, title, description }); else if (type === "error") this.err(title, description); else this.ok(title, description); },
  };

  // ---------- WebAuthn ----------
  const b64u = {
    toBuf(s) { s = s.replace(/-/g, "+").replace(/_/g, "/"); while (s.length % 4) s += "="; const b = atob(s); const u = new Uint8Array(b.length); for (let i = 0; i < b.length; i++) u[i] = b.charCodeAt(i); return u.buffer; },
    fromBuf(buf) { const u = new Uint8Array(buf); let s = ""; for (const c of u) s += String.fromCharCode(c); return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, ""); },
  };
  function creationOptions(pk) {
    if (window.PublicKeyCredential && PublicKeyCredential.parseCreationOptionsFromJSON) return PublicKeyCredential.parseCreationOptionsFromJSON(pk);
    const o = structuredClone(pk);
    o.challenge = b64u.toBuf(o.challenge);
    o.user.id = b64u.toBuf(o.user.id);
    (o.excludeCredentials || []).forEach((c) => { c.id = b64u.toBuf(c.id); });
    return o;
  }
  function requestOptions(pk) {
    if (window.PublicKeyCredential && PublicKeyCredential.parseRequestOptionsFromJSON) return PublicKeyCredential.parseRequestOptionsFromJSON(pk);
    const o = structuredClone(pk);
    o.challenge = b64u.toBuf(o.challenge);
    (o.allowCredentials || []).forEach((c) => { c.id = b64u.toBuf(c.id); });
    return o;
  }
  function credJSON(cred) {
    if (cred.toJSON) return cred.toJSON();
    const r = cred.response, out = { id: cred.id, rawId: b64u.fromBuf(cred.rawId), type: cred.type, response: { clientDataJSON: b64u.fromBuf(r.clientDataJSON) }, clientExtensionResults: cred.getClientExtensionResults() };
    if (r.attestationObject) { out.response.attestationObject = b64u.fromBuf(r.attestationObject); if (r.getTransports) out.response.transports = r.getTransports(); }
    if (r.authenticatorData) { out.response.authenticatorData = b64u.fromBuf(r.authenticatorData); out.response.signature = b64u.fromBuf(r.signature); if (r.userHandle) out.response.userHandle = b64u.fromBuf(r.userHandle); }
    if (cred.authenticatorAttachment) out.authenticatorAttachment = cred.authenticatorAttachment;
    return out;
  }
  function unwrapPK(data) { return data.publicKey && data.publicKey.publicKey ? data.publicKey.publicKey : data.publicKey; }
  async function registerKey(kind, name) {
    const begin = await api("POST", "/api/auth/register/begin", { kind, name });
    const cred = await navigator.credentials.create({ publicKey: creationOptions(unwrapPK(begin)) });
    return api("POST", "/api/auth/register/finish", credJSON(cred));
  }
  async function signInWithKey() {
    const begin = await api("POST", "/api/auth/login/begin");
    const cred = await navigator.credentials.get({ publicKey: requestOptions(unwrapPK(begin)) });
    return api("POST", "/api/auth/login/finish", credJSON(cred));
  }
  const webauthnCancelled = (e) => e && (e.name === "NotAllowedError" || e.name === "AbortError");

  // ---------- reserve lane geometry: now to the end of Sunday ----------
  function laneEnd(now = Date.now()) {
    const d = new Date(now);
    const dow = (d.getDay() + 6) % 7; // Monday = 0
    const end = new Date(d.getFullYear(), d.getMonth(), d.getDate() + (6 - dow) + 1, 0, 0, 0, 0);
    if (end.getTime() - now < 2 * DAY) end.setDate(end.getDate() + 7); // late in the week, show next week too
    return end.getTime();
  }
  function axisDays(now = Date.now()) {
    const end = laneEnd(now), out = [];
    const d = new Date(now); d.setHours(0, 0, 0, 0); d.setDate(d.getDate() + 1);
    while (d.getTime() < end) {
      const start = d.getTime(), next = start + DAY;
      out.push({ label: d.toLocaleDateString("en-GB", { weekday: "short" }), start, mid: (start + Math.min(next, end)) / 2 });
      d.setDate(d.getDate() + 1);
    }
    return { end, days: out };
  }

  const PROVIDERS = {
    claude: { name: "Claude", how: "Sign in with your Claude account in the browser", icon: "claude" },
    codex: { name: "Codex", how: "Sign in with ChatGPT in the browser", icon: "codex" },
    grok: { name: "Grok", how: "Enter a code at x.ai", icon: "grok" },
    cursor: { name: "Cursor", how: "Sign in with Cursor, or paste an API key", icon: "cursor" },
    opencode: { name: "OpenCode Console", how: "Paste a service account key", icon: "opencode" },
    openai: { name: "OpenAI-compatible", how: "Any /v1 API: OpenRouter, Groq, a local llama.cpp", icon: "openai" },
  };

  // "7 days" → "7-day", "5 hours" → "5-hour"; other labels stay as they are.
  function noun(label) {
    const m = /^(\d+) (day|hour|minute)s?(.*)$/.exec(label || "");
    return m ? `${m[1]}-${m[2]}${m[3]}` : (label || "");
  }
  const cap = (s) => (s ? s[0].toUpperCase() + s.slice(1) : s);

  window.Mainspring = { fmt, icon, MARK, SPINNER, api, T, PROVIDERS };

  document.addEventListener("alpine:init", () => {
    Alpine.data("app", () => ({
      fmt, icon, MARK, SPINNER, PROVIDERS, ROUTES, noun, api, T,
      now: Date.now(),
      route: "pool",
      auth: null,          // /api/auth/state
      authError: "",
      authBusy: false,
      setup: { code: "", verified: false, name: "", step: 1, error: "", backupName: "" },
      endpoint: location.origin + "/v1",
      pool: null, poolError: "",
      sessions: null, sessionsError: "",
      meter: null, meterError: "", meterRange: "today",
      keys: null, keysError: "",
      models: null, modelsError: "", modelSearch: "", modelSource: "",
      settings: null, settingsError: "", authKeys: [],
      drawer: null, drawerData: null,
      dialog: null,        // { kind, ...state }
      checking: {},
      timers: [],

      async init() {
        window.addEventListener("hashchange", () => this.go());
        window.addEventListener("ms:signed-out", () => this.loadAuth());
        setInterval(() => { this.now = Date.now(); }, 1000);
        await this.loadAuth();
        this.go();
        setInterval(() => this.poll(), 10000);
        document.addEventListener("keydown", (e) => {
          if (e.key === "Escape") { if (this.dialog) this.closeDialog(); else if (this.drawer) this.closeDrawer(); }
        });
      },
      async loadAuth() {
        try { this.auth = await api("GET", "/api/auth/state"); this.authError = ""; }
        catch (e) { this.authError = e.message; }
      },
      get signedIn() { return this.auth && this.auth.signedIn; },
      go() {
        const h = location.hash.replace(/^#\/?/, "") || "pool";
        this.route = ROUTES.includes(h) ? h : "pool";
        this.closeDrawer();
        if (this.signedIn) this.load(this.route);
      },
      load(r) {
        const map = { pool: this.loadPool, sessions: this.loadSessions, keys: this.loadKeys, meter: this.loadMeter, sources: this.loadSources, settings: this.loadSettings };
        (map[r] || (() => {})).call(this);
        if (r !== "pool" && !this.pool) this.loadPool();
      },
      poll() {
        if (!this.signedIn || document.hidden || this.dialog) return;
        if (this.route === "pool") this.loadPool();
        if (this.route === "sessions") this.loadSessions();
        if (this.drawer) this.openDrawer(this.drawer, true);
      },

      // ----- data -----
      async loadPool() { try { this.pool = await api("GET", "/api/pool"); this.poolError = ""; } catch (e) { this.poolError = e.message; } },
      async loadSessions() { try { this.sessions = await api("GET", "/api/sessions"); this.sessionsError = ""; } catch (e) { this.sessionsError = e.message; } },
      async loadKeys() { try { this.keys = await api("GET", "/api/keys"); this.keysError = ""; } catch (e) { this.keysError = e.message; } },
      async loadMeter() { try { this.meter = await api("GET", "/api/meter?range=" + this.meterRange); this.meterError = ""; } catch (e) { this.meterError = e.message; } },
      async loadSources() {
        try { this.models = await api("GET", "/api/models"); this.modelsError = ""; } catch (e) { this.modelsError = e.message; }
        if (!this.pool) this.loadPool();
      },
      async loadSettings() {
        try { this.settings = await api("GET", "/api/settings"); this.settingsError = ""; } catch (e) { this.settingsError = e.message; }
        try { this.authKeys = (await api("GET", "/api/auth/keys")).keys || []; } catch { this.authKeys = []; }
      },

      // ----- pool helpers -----
      get attention() { return this.pool ? this.pool.attention : 0; },
      get liveSessions() { return this.pool ? this.pool.sessions : 0; },
      groupReason(g) {
        const next = g.accounts.find((a) => a.id === g.next_up);
        if (!next) {
          return g.back_at ? `Every ${g.name} account is out. The first one is back ${fmt.in(g.back_at, this.now)}, at ${fmt.clock(g.back_at)}` : `No ${g.name} account can take new sessions`;
        }
        const w = next.long || next.short;
        if (!w || w.left == null) return `<b>${this.esc(next.label)}</b> goes first. Mainspring hasn't read its limits yet`;
        const when = w.resets_at ? `its ${noun(w.label)} limit resets ${fmt.in(w.resets_at, this.now)}` : `its ${noun(w.label)} limit`;
        return `<b>${this.esc(next.label)}</b> goes first: ${when}, ${fmt.pct(w.left)} left`;
      },
      statusOf(a, g) {
        const codexNeutral = g && g.provider !== "claude";
        switch (a.status) {
          case "next": return { mark: codexNeutral ? MARK.nextNeutral : MARK.next, cls: codexNeutral ? "next-neutral" : "next", text: "Next up" };
          case "needs_sign_in": return { mark: MARK.needs, cls: "needs", text: "Needs sign-in" };
          case "benched": return { mark: MARK.benched, cls: "quiet", text: "Benched" };
          case "cooling": return { mark: MARK.cooling, cls: "", text: a.cool_until ? "Cooling until " + fmt.clock(a.cool_until) : "Cooling" };
        }
        if (a.stale) return { mark: MARK.stale, cls: "quiet", text: "Stale" };
        return { mark: MARK.active, cls: "", text: "Active" };
      },
      isAccent(a, g) { return a.status === "next" && g && g.provider === "claude"; },
      rowMessage(a) {
        if (a.status === "needs_sign_in") return `${PROVIDERS[a.provider]?.name || a.provider} rejected its refresh token${a.sign_in_at ? " at " + fmt.clock(a.sign_in_at) : ""}, so no sessions go here`;
        if (a.status === "benched") return a.benched_at ? `You took it out of rotation on ${fmt.day(a.benched_at)}` : "You took it out of rotation";
        return "";
      },
      lane(a) {
        const w = a.long;
        const W = 204, now = this.now, end = laneEnd(now);
        const x = (t) => Math.max(0, Math.min(W, ((t - now) / (end - now)) * W));
        if (!w || !w.resets_at) return { svg: "", label: "", x: 0 };
        const rx = Math.max(3, x(w.resets_at));
        const left = w.left == null ? 0 : w.left;
        const fill = (rx * left) / 100;
        const cooling = a.status === "cooling" && w.spent;
        const color = this.isAccent(a, { provider: a.provider }) ? "#3B9BFF" : a.stale ? "#737373" : "#A1A1A1";
        const pin = this.isAccent(a, { provider: a.provider }) ? "#3B9BFF" : a.stale ? "#737373" : "#A1A1A1";
        let ticks = "";
        for (const d of axisDays(now).days) ticks += `<rect x="${x(d.start).toFixed(1)}" y="5" width="1" height="10" fill="#363636"/>`;
        const hatch = cooling ? `<defs><pattern id="h${a.id}" width="4" height="4" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><rect width="1.5" height="4" fill="#737373"/></pattern></defs><rect x="0" y="7" width="${rx}" height="6" rx="3" fill="url(#h${a.id})"/>` : "";
        const svg = `<svg width="${W}" height="20" viewBox="0 0 ${W} 20" aria-hidden="true"><rect x="0" y="3" width="1" height="14" fill="#737373"/>${ticks}<rect x="0" y="7" width="${rx}" height="6" rx="3" fill="#363636"/>${cooling ? hatch : fill > 0 ? `<rect x="0" y="7" width="${Math.max(3, fill)}" height="6" rx="3" fill="${color}"/>` : ""}<rect x="${rx - 1}" y="3" width="2" height="14" rx="1" fill="${pin}"/></svg>`;
        return { svg, label: fmt.in(w.resets_at, now), x: rx + 8 };
      },
      axis() {
        const now = this.now, end = laneEnd(now), W = 204;
        return axisDays(now).days.map((d) => ({ label: d.label, x: Math.max(16, Math.min(W - 4, ((d.mid - now) / (end - now)) * W)) }));
      },
      shortHeader(g) { const a = g.accounts.find((x) => x.short); return a ? cap(noun(a.short.label)) + " left" : "Short window left"; },
      longHeader(g) { const a = g.accounts.find((x) => x.long); return a ? cap(noun(a.long.label)) + " left" : "Long window left"; },
      shortRel(a) {
        const w = a.short;
        if (!w) return "—";
        if (w.left === 100 && (!w.resets_at || w.resets_at < this.now)) return "Full";
        return w.resets_at ? fmt.in(w.resets_at, this.now) : "—";
      },
      async setRotation(a, on) {
        const before = a.in_rotation;
        a.in_rotation = on;
        try {
          await api("POST", `/api/accounts/${a.id}/rotation`, { on });
          T.ok(on ? `${a.label} is back in rotation` : `Took ${a.label} out of rotation`);
          this.loadPool();
          if (this.drawer === a.id) this.openDrawer(a.id, true);
        } catch (e) { a.in_rotation = before; T.err(`Couldn't change ${a.label}`, e.message); }
      },
      async check(a) {
        if (this.checking[a.id]) return;
        this.checking[a.id] = true;
        const name = PROVIDERS[a.provider]?.name || a.provider;
        const id = T.busy(`Checking ${a.label}`, `Asking ${name} for its current limits`);
        try {
          const res = await api("POST", `/api/accounts/${a.id}/check`);
          const acct = res.account, w = acct.long || acct.short;
          T.done(id, "success", `${a.label} is up to date`, w && w.left != null ? `${fmt.pct(w.left)} of the ${w.label} limit left${w.resets_at ? ", resets " + fmt.in(w.resets_at) : ""}` : undefined);
          this.loadPool();
          if (this.drawer === a.id) this.openDrawer(a.id, true);
        } catch (e) {
          T.done(id, "error", `Couldn't check ${a.label}`, e.message);
        } finally { this.checking[a.id] = false; }
      },
      async openDrawer(id, quiet) {
        this.drawer = id;
        try { this.drawerData = await api("GET", `/api/accounts/${id}`); }
        catch (e) { if (!quiet) T.err("Couldn't open that account", e.message); this.drawer = null; }
      },
      closeDrawer() { this.drawer = null; this.drawerData = null; },

      // ----- dialogs -----
      openDialog(kind, state = {}) { this.dialog = { kind, busy: false, error: "", ...state }; },
      closeDialog() {
        const d = this.dialog;
        if (d && d.kind === "login" && d.provider && (d.provider === "claude" || d.provider === "codex") && !d.done) api("POST", `/api/login/${d.provider}/cancel`).catch(() => {});
        if (d && d.pollTimer) clearInterval(d.pollTimer);
        this.dialog = null;
      },
      async confirm(title, body, action, danger = true) {
        return new Promise((resolve) => { this.openDialog("confirm", { title, body, action, danger, resolve }); });
      },
      answer(ok) { const r = this.dialog && this.dialog.resolve; this.dialog = null; if (r) r(ok); },

      // Add account
      addAccount() { this.openDialog("choose"); },
      async startLogin(provider) {
        if (provider === "openai") return this.openDialog("openai", { name: "", base_url: "", key: "" });
        if (provider === "opencode") return this.openDialog("secret", { provider, secret: "" });
        const d = { provider, mode: "browser", start: null, callback: "", secret: "", done: false };
        this.openDialog("login", d);
        if (provider === "cursor") return; // choose a mode first
        await this.beginLogin();
      },
      async beginLogin(mode) {
        const d = this.dialog;
        if (!d) return;
        d.error = ""; d.busy = true; d.start = null;
        try {
          d.start = await api("POST", `/api/login/${d.provider}/start`, { mode: mode || "" });
          if (d.start.kind === "callback" || d.start.kind === "redirect") {
            if (d.start.url) window.open(d.start.url, "_blank", "noopener");
            d.pollTimer = setInterval(() => this.pollLogin(), 1500);
          } else if (d.start.kind === "device") {
            if (d.provider === "cursor" && d.start.url) window.open(d.start.url, "_blank", "noopener");
            this.waitDevice();
          }
        } catch (e) { d.error = e.message; }
        d.busy = false;
      },
      async pollLogin() {
        const d = this.dialog;
        if (!d || d.kind !== "login" || !d.start) return;
        try {
          const st = await api("GET", `/api/login/${d.provider}/status?state=${encodeURIComponent(d.start.state || "")}`);
          if (st.done) {
            clearInterval(d.pollTimer);
            if (st.error) { d.error = st.error; return; }
            this.loginDone(d.provider, st.label, st.updated);
          }
        } catch { /* keep polling */ }
      },
      async finishCallback() {
        const d = this.dialog;
        d.error = ""; d.busy = true;
        try { const res = await api("POST", `/api/login/${d.provider}/finish`, { callback: d.callback }); this.loginDone(d.provider, res.label, res.updated); }
        catch (e) { d.error = e.message; }
        if (this.dialog) this.dialog.busy = false;
      },
      async waitDevice() {
        const d = this.dialog;
        try {
          const res = await api("POST", `/api/login/${d.provider}/device`, { device_code: d.start.state || "" });
          if (this.dialog === d) this.loginDone(d.provider, res.label, res.updated);
        } catch (e) { if (this.dialog === d) d.error = e.message; }
      },
      async submitSecret() {
        const d = this.dialog;
        d.error = ""; d.busy = true;
        try { const res = await api("POST", `/api/login/${d.provider}/secret`, { secret: d.secret }); this.loginDone(d.provider, res.label, res.updated); }
        catch (e) { d.error = e.message; }
        if (this.dialog) this.dialog.busy = false;
      },
      async submitOpenAI() {
        const d = this.dialog;
        d.error = ""; d.busy = true;
        try { const res = await api("POST", "/api/sources/openai", { name: d.name, base_url: d.base_url, key: d.key }); this.loginDone("openai", res.label, false); }
        catch (e) { d.error = e.message; }
        if (this.dialog) this.dialog.busy = false;
      },
      loginDone(provider, label, updated) {
        if (this.dialog) { this.dialog.done = true; if (this.dialog.pollTimer) clearInterval(this.dialog.pollTimer); }
        this.dialog = null;
        T.ok(updated ? `Signed ${label} in again` : `Added ${label}`, updated ? "It was already in the pool, so Mainspring updated it" : undefined);
        this.loadPool();
        if (this.route === "sources") this.loadSources();
      },
      slugPreview(name) { return (name || "name").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "name"; },

      async removeAccount(a) {
        const ok = await this.confirm(`Disconnect ${a.label}?`, `Mainspring signs out of this ${PROVIDERS[a.provider]?.name || ""} account and deletes its tokens.${a.sessions ? ` Its ${fmt.plural(a.sessions, "live session", "live sessions")} move to another account.` : ""} To stop new sessions without signing out, take it out of rotation instead`, "Disconnect");
        if (!ok) return;
        try { await api("DELETE", `/api/accounts/${a.id}`); this.closeDrawer(); T.ok(`Disconnected ${a.label}`); this.loadPool(); if (this.route === "sources") this.loadSources(); }
        catch (e) { T.err(`Couldn't disconnect ${a.label}`, e.message); }
      },
      async renameAccount(a) {
        const d = { title: "Rename account", value: a.label, onSave: async (v) => {
          await api("POST", `/api/accounts/${a.id}/rename`, { name: v });
          T.ok("Renamed to " + v); this.loadPool(); this.openDrawer(a.id, true);
        } };
        this.openDialog("rename", d);
      },
      async saveRename() {
        const d = this.dialog;
        d.busy = true; d.error = "";
        try { await d.onSave(d.value.trim()); this.dialog = null; } catch (e) { d.error = e.message; d.busy = false; }
      },

      // ----- keys -----
      newKey() { this.openDialog("newkey", { name: "", scope: [], models: "", cap: "" }); },
      toggleScope(p) { const s = this.dialog.scope; const i = s.indexOf(p); if (i >= 0) s.splice(i, 1); else s.push(p); },
      async createKey() {
        const d = this.dialog;
        d.busy = true; d.error = ""; d.fieldError = "";
        try {
          const res = await api("POST", "/api/keys", { name: d.name.trim(), providers: d.scope, models: d.models, daily_cap: d.cap || undefined });
          this.openDialog("keycreated", { secret: res.secret, key: res.key, tab: "claude", copied: false });
          this.loadKeys();
        } catch (e) {
          if (e.code === "name_taken" || e.code === "name_invalid") { d.fieldError = e.message; } else if (e.code === "cap_invalid") { d.capError = e.message; } else { d.error = e.message; }
          d.busy = false;
        }
      },
      async revokeKey(k) {
        const ok = await this.confirm(`Revoke ${k.name}?`, `Mainspring refuses <span class="mono strong">${this.esc(k.prefix)}…</span> from now on${k.sessions ? `, so its ${fmt.plural(k.sessions, "live session ends", "live sessions end")} at the next request` : ""}. You can't undo this`, `Revoke ${k.name}`);
        if (!ok) return;
        try { await api("POST", `/api/keys/${k.id}/revoke`); T.ok(`Revoked ${k.name}`, k.sessions ? `Its ${fmt.plural(k.sessions, "session ends", "sessions end")} at the next request` : undefined); this.loadKeys(); }
        catch (e) { T.err(`Couldn't revoke ${k.name}`, e.message); }
      },
      snippet(tab, secret) {
        const origin = location.origin;
        if (tab === "claude") return `{\n  "env": {\n    "ANTHROPIC_BASE_URL": "${origin}",\n    "ANTHROPIC_AUTH_TOKEN": "${secret}"\n  }\n}`;
        if (tab === "codex") return `model_provider = "mainspring"\n\n[model_providers.mainspring]\nname = "Mainspring"\nbase_url = "${origin}/v1"\nwire_api = "responses"\nrequires_openai_auth = false\nsupports_websockets = true\nexperimental_bearer_token = "${secret}"`;
        return `Base URL   ${origin}/v1\nAPI key    ${secret}`;
      },
      snippetPath(tab) { return tab === "claude" ? "Merge into ~/.claude/settings.json" : tab === "codex" ? "Add to ~/.codex/config.toml" : "Any tool that speaks the OpenAI API"; },

      // ----- settings -----
      async saveSetting(patch, msg) {
        try { this.settings = await api("PUT", "/api/settings", patch); if (msg) T.ok(msg); }
        catch (e) { T.err("Couldn't save that setting", e.message); this.loadSettings(); }
      },
      async createHubKey() {
        try { const res = await api("POST", "/api/settings/hub-key"); this.settings = res.settings; this.openDialog("hubkey", { secret: res.secret }); }
        catch (e) { T.err("Couldn't create a management key", e.message); }
      },
      async removeHubKey() {
        const ok = await this.confirm("Delete the management key?", "Dashboards that use it stop seeing your limits. You can create a new one any time", "Delete key");
        if (!ok) return;
        try { this.settings = await api("DELETE", "/api/settings/hub-key"); T.ok("Deleted the management key"); } catch (e) { T.err("Couldn't delete it", e.message); }
      },
      addSignInKey(keyKind) { this.openDialog("addkey", { keyKind, name: "", waiting: false }); },
      async submitSignInKey() {
        const d = this.dialog;
        d.error = ""; d.waiting = true;
        try {
          const res = await registerKey(d.keyKind, d.name.trim());
          this.dialog = null;
          await this.loadSettings(); await this.loadAuth();
          T.ok(`Added ${res.key.name}`, `You have ${fmt.plural(this.authKeys.length, "sign-in key", "sign-in keys")}`);
        } catch (e) {
          d.waiting = false;
          d.error = webauthnCancelled(e) ? "The browser closed the key prompt. Try again" : e.message;
        }
      },
      async removeSignInKey(k) {
        const ok = await this.confirm(`Remove ${k.name}?`, "You won't be able to sign in with it any more", "Remove key");
        if (!ok) return;
        try { await api("DELETE", `/api/auth/keys/${k.id}`); T.ok(`Removed ${k.name}`); this.loadSettings(); this.loadAuth(); }
        catch (e) { T.err(`Couldn't remove ${k.name}`, e.code === "last_key" ? "It's your last key. Add another one first" : e.message); }
      },
      async renameSignInKey(k) {
        this.openDialog("rename", { title: "Rename key", value: k.name, onSave: async (v) => { await api("POST", `/api/auth/keys/${k.id}/rename`, { name: v }); T.ok("Renamed to " + v); this.loadSettings(); } });
      },
      async setRequireKey(on) {
        try { await api("PUT", "/api/auth/require-key", { on }); await this.loadAuth(); T.ok(on ? "The panel now asks for your key" : "The panel relies on Umbrel's login"); }
        catch (e) { T.err("Couldn't change that", e.message); }
      },
      async lock() {
        try { await api("POST", "/api/auth/logout"); } catch { /* ignore */ }
        await this.loadAuth();
      },

      // ----- sign-in and setup -----
      async signIn() {
        this.authBusy = true; this.authError = "";
        try { await signInWithKey(); await this.loadAuth(); this.go(); }
        catch (e) { this.authError = webauthnCancelled(e) ? "" : e.code === "key_not_added" ? "That key isn't added to this Mainspring" : e.message; }
        this.authBusy = false;
      },
      async verifySetup() {
        this.setup.error = "";
        try { await api("POST", "/api/auth/setup/verify", { code: this.setup.code }); this.setup.verified = true; this.setup.step = 2; }
        catch (e) { this.setup.error = e.message; }
      },
      async setupKey(kind, backup) {
        const name = (backup ? this.setup.backupName : this.setup.name).trim();
        if (!name) { this.setup.error = "Give the key a name so you can tell your keys apart"; return; }
        this.setup.error = ""; this.authBusy = true;
        try {
          await registerKey(kind, name);
          await this.loadAuth();
          if (backup) { this.setup.step = 4; this.go(); }
          else this.setup.step = 3;
        } catch (e) { this.setup.error = webauthnCancelled(e) ? "The browser closed the key prompt. Try again" : e.message; }
        this.authBusy = false;
      },
      finishSetup() { this.setup.step = 4; this.go(); },
      get gateView() {
        if (!this.auth) return this.authError ? "error" : "loading";
        if (this.auth.signedIn && this.setup.step !== 3) return "app";
        if (!this.auth.secureContext) return "insecure";
        if (this.auth.setupNeeded || this.setup.step === 2 || this.setup.step === 3) return "setup";
        return "signin";
      },

      // ----- misc -----
      async copy(text, what) {
        try { await navigator.clipboard.writeText(text); T.ok(`Copied ${what}`); }
        catch { T.err("Couldn't copy", "Select the text and copy it yourself"); }
      },
      esc(s) { return String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]); },
      activityText(ev) {
        const d = ev.data || {}, b = (s) => `<b>${this.esc(s)}</b>`;
        switch (ev.kind) {
          case "limit": return `${b(d.account)} hit its ${this.esc(noun(d.window || "usage"))} limit.` + (d.moved ? ` Moved ${fmt.plural(d.moved, "session", "sessions")} to ${b(d.to)}` : (d.until ? ` Back ${fmt.in(d.until, ev.ts)}` : ""));
          case "moved": return `Moved ${fmt.plural(d.moved || 1, "session", "sessions")} from ${b(d.from)} to ${b(d.to)}`;
          case "sign_in": return `${PROVIDERS[ev.provider]?.name || ev.provider} rejected the refresh token for ${b(d.account)}. It needs you to sign in again`;
          case "all_out": return `Every ${PROVIDERS[ev.provider]?.name || ev.provider} account was out` + (d.back_at ? `. The first one is back ${fmt.in(d.back_at, ev.ts)}` : "");
          case "added": return d.updated ? `Signed ${b(d.account)} in again` : `Added ${b(d.account)}`;
          case "removed": return `Disconnected ${b(d.account)}`;
          case "benched": return d.in_rotation ? `Put ${b(d.account)} back in rotation` : `Took ${b(d.account)} out of rotation`;
          case "key_added": return `Created the key ${b(d.key)}`;
          case "key_revoke": return `Revoked the key ${b(d.key)}`;
        }
        return this.esc(ev.kind);
      },
      filteredModels() {
        if (!this.models) return [];
        const q = this.modelSearch.trim().toLowerCase();
        return this.models.models.filter((m) => (!q || m.id.toLowerCase().includes(q) || (m.name || "").toLowerCase().includes(q)) && (!this.modelSource || m.served_by === this.modelSource));
      },
      modelSources() { return this.models ? [...new Set(this.models.models.map((m) => m.served_by).filter(Boolean))] : []; },
      chart() {
        const m = this.meter;
        if (!m || !m.daily) return { bars: [], max: 0, ticks: [] };
        const max = Math.max(1, ...m.daily.map((d) => d.tokens));
        const nice = Math.pow(10, Math.floor(Math.log10(max)));
        const top = Math.ceil(max / nice) * nice;
        const n = m.daily.length, w = 1000, h = 180, gap = 12, bw = (w - gap * (n - 1)) / n;
        const today = new Date().toISOString().slice(0, 10);
        const from = m.range === "today" ? n - 1 : n - (m.range === "7d" ? 7 : 14);
        return {
          top,
          bars: m.daily.map((d, i) => ({ x: i * (bw + gap), w: bw, h: (d.tokens / top) * h, y: h - (d.tokens / top) * h, label: i === n - 1 ? "Today" : new Date(d.day + "T12:00:00").getDate(), tokens: d.tokens, inRange: i >= from, today: d.day === today })),
        };
      },
      chartSVG() {
        const c = this.chart();
        if (!c.bars.length) return "";
        let out = '<svg viewBox="0 0 1000 220" role="img" aria-label="Tokens per day">';
        for (const b of c.bars) {
          const fill = b.today ? "#3B9BFF" : b.inRange ? "#6B6B6B" : "#3A3A3A";
          const h = Math.max(b.h, b.tokens ? 2 : 0);
          out += `<rect x="${b.x.toFixed(1)}" y="${(180 - h).toFixed(1)}" width="${b.w.toFixed(1)}" height="${h.toFixed(1)}" rx="3" fill="${fill}"><title>${fmt.tokens(b.tokens)} tokens</title></rect>`;
          out += `<text x="${(b.x + b.w / 2).toFixed(1)}" y="212" text-anchor="middle" fill="#737373" font-size="22" font-family="Geist, sans-serif">${b.label}</text>`;
        }
        return out + "</svg>";
      },
      dialogTitle() {
        const d = this.dialog; if (!d) return "";
        if (d.kind === "login" || d.kind === "secret") return "Add " + (PROVIDERS[d.provider]?.name || d.provider) + (d.provider === "opencode" ? "" : " account");
        return "";
      },
    }));
  });
})();
