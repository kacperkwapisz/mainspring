package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/provider"
	"github.com/kacperkwapisz/mainspring/internal/route"
	"github.com/kacperkwapisz/mainspring/internal/translate"
)

// Images run on the Codex accounts in the pool: a ChatGPT plan includes image
// generation, which Codex's own image tool uses. A call goes to the account
// the calling session is on, else the one the router picks, and moves on when
// that account can't make images right now.

// InboundImages is the OpenAI Images API (/v1/images/generations and /edits).
const InboundImages domain.InboundFormat = "openai.images"

// imageMaker is implemented by providers with an Images API on the account.
type imageMaker interface {
	Images(ctx context.Context, cred domain.Credential, edit bool, body map[string]any, inbound map[string]string) (*http.Response, error)
}

// maxImageResponse bounds a buffered Images answer. Pictures come back as
// base64, a few MB each, and a request may ask for several.
const maxImageResponse = 128 << 20

// imageHeaderNames are the upstream response headers an Images answer keeps.
var imageHeaderNames = []string{"X-Codex-Imagegen-Request-Id"}

// Images generates (edit false) or edits pictures. body is the OpenAI Images
// API request as JSON; it is sent as the client wrote it.
func Images(ctx context.Context, hub *Hub, key domain.LocalKey, headers map[string]string, edit bool, body map[string]any) (Result, error) {
	started := time.Now().UnixMilli()
	req := Request{Inbound: InboundImages, Body: body, Key: key, Stream: body["stream"] == true, InboundHeaders: headers}
	routeName := "/v1/images/generations"
	if edit {
		routeName = "/v1/images/edits"
	}
	model := requestedModel(body)
	if model == "" {
		return failOut(req, 400, httpx.KindRequest, "", "", "model is required", `set "model" in the request body, for example "gpt-image-2"`), nil
	}
	if modelDisabled(DisabledModels(hub), model) {
		return failOut(req, 404, httpx.KindModel, "", model, model+" is turned off in Mainspring", "turn it on under Sources → Models in the panel"), nil
	}
	p := domain.ProviderCodex
	if res, capped, err := dailyCapReached(hub, req, model); err != nil || capped {
		return res, err
	}
	if err := hub.Keys.Allows(key, p, model); err != nil {
		return failFromExec(req, p, model, provider.ExecuteResult{Status: 403, Message: err.Error()}), nil
	}
	ex, ok := hub.Executors[p].(imageMaker)
	if !ok {
		return failOut(req, 503, httpx.KindNoCredential, string(p), model, "no Codex account can make images", "connect a ChatGPT account under Sources"), nil
	}
	all, err := hopCredentials(hub, p, model)
	if err != nil {
		return Result{}, err
	}
	if len(all) == 0 {
		return failOut(req, 503, httpx.KindNoCredential, string(p), model, "no Codex account is connected", "connect a ChatGPT account under Sources; images run on its plan"), nil
	}
	router := hub.router()
	creds, backAt := imageCandidates(router, p, all, route.SessionKey(headers, nil, key.ID))
	if len(creds) == 0 {
		res := allResting(req, p, model, backAt)
		recordFail(hub, req, p, model, "", started, res)
		return res, nil
	}
	var last provider.ExecuteResult
	for _, cred := range creds {
		if nearExpiry(cred) {
			if refreshed, err := refreshCredential(ctx, hub, hub.Executors[p], cred); err == nil {
				cred = refreshed
			} else {
				logRefresh(p, cred.Label, routeName, err)
				if signInRejected(hub, cred, err) {
					last = provider.ExecuteResult{OK: false, Status: 401, Retryable: true, Message: "sign-in rejected"}
					continue
				}
			}
		}
		res, err := ex.Images(ctx, cred, edit, body, headers)
		if err == nil && res.StatusCode == 401 {
			res.Body.Close()
			refreshed, rerr := refreshCredential(ctx, hub, hub.Executors[p], cred)
			if rerr != nil {
				logRefresh(p, cred.Label, routeName, rerr)
				if !signInRejected(hub, cred, rerr) {
					router.AuthRejected(cred.ID)
				}
				last = provider.ExecuteResult{OK: false, Status: 401, Retryable: true, Message: "sign-in rejected"}
				continue
			}
			cred = refreshed
			res, err = ex.Images(ctx, cred, edit, body, headers)
		}
		if err != nil {
			last = provider.FailFromErr(err)
			if last.Retryable {
				continue
			}
			break
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			last = provider.FailFromResponse(res)
			if last.Status == 401 {
				router.AuthRejected(cred.ID)
			}
			if provider.IsRetryableStatus(last.Status) {
				continue
			}
			break
		}
		// No router.Succeeded: a picture says nothing about the account's chat
		// limits, so it doesn't clear their backoff either.
		out := accountHeaders(hub, keepHeaders(res.Header, imageHeaderNames), p, cred, "", "")
		if strings.HasPrefix(strings.ToLower(res.Header.Get("Content-Type")), "text/event-stream") {
			return Result{OK: true, Status: res.StatusCode, Stream: relayImageStream(ctx, hub, req, cred, model, started, routeName, res.Body), Header: out}, nil
		}
		raw, err := io.ReadAll(io.LimitReader(res.Body, maxImageResponse+1))
		res.Body.Close()
		if err != nil || len(raw) > maxImageResponse {
			msg := "the image answer was cut off"
			if err == nil {
				msg = "the image answer is larger than Mainspring passes on"
			}
			last = provider.ExecuteResult{OK: false, Status: 502, Message: msg}
			break
		}
		if !json.Valid(raw) {
			last = provider.ExecuteResult{OK: false, Status: 502, Message: "Codex answered with something that isn't JSON"}
			break
		}
		httpx.LogOK(string(p), model, routeName)
		record(hub, req, p, model, cred.ID, started, "ok", imageUsage(raw), "")
		return Result{OK: true, Status: res.StatusCode, Raw: raw, Header: out}, nil
	}
	if last.Status == 0 {
		last = provider.ExecuteResult{Status: 502, Message: "every Codex account failed"}
	}
	res := withIncident(hub, p, last.Status, failFromExec(req, p, model, last))
	recordFail(hub, req, p, model, "", started, res)
	return res, nil
}

// imageCandidates orders the accounts an Images call may try: the router's
// order first, the calling session's account leading. Image quota is separate
// from chat, so an account resting for a spent chat window comes after them
// instead of being left out. Benched accounts, ones that need signing in again
// and ones resting for any other reason (a rejected token, a plain rate
// limit) stay out.
func imageCandidates(router *route.Engine, p domain.ProviderID, all []domain.Credential, sessionKey string) ([]domain.Credential, int64) {
	ordered, backAt := router.Order(p, all, sessionKey)
	out := make([]domain.Credential, 0, len(all))
	seen := map[string]bool{}
	for _, c := range ordered {
		out = append(out, c.Credential)
		seen[c.Credential.ID] = true
	}
	now := time.Now().UnixMilli()
	for _, c := range all {
		if seen[c.ID] {
			continue
		}
		st := router.Get(c.ID)
		if st.Benched || st.NeedsSignIn || (st.Cooling(now) && st.CoolWindow == "") {
			continue
		}
		out = append(out, c)
	}
	return out, backAt
}

// imageUsage reads the usage from an Images answer without decoding the
// pictures.
func imageUsage(raw []byte) translate.Usage {
	var v struct {
		Usage map[string]any `json:"usage"`
	}
	_ = json.Unmarshal(raw, &v)
	u, _ := translate.ParseUsage(v.Usage)
	return u
}

// dailyCapReached refuses a request when its key has used its daily tokens.
func dailyCapReached(hub *Hub, req Request, model string) (Result, bool, error) {
	if req.Key.DailyCap == nil {
		return Result{}, false, nil
	}
	used, err := hub.Usage.TodayTokensForKey(req.Key.ID, Zone(hub))
	if err != nil {
		return Result{}, false, err
	}
	if used >= *req.Key.DailyCap {
		return failOut(req, 429, httpx.KindCap, "", model, "daily token cap reached", "wait until midnight ("+Zone(hub).String()+"), or raise the key's cap in the panel"), true, nil
	}
	return Result{}, false, nil
}

func keepHeaders(h http.Header, names []string) http.Header {
	out := http.Header{}
	for _, name := range names {
		if v := h.Get(name); v != "" {
			out.Set(name, v)
		}
	}
	return out
}

// relayImageStream passes a streamed Images answer on event by event, as it
// came, and records it: the usage its final event reports, and an error when
// the stream ends before a completed event or the client leaves.
func relayImageStream(ctx context.Context, hub *Hub, req Request, cred domain.Credential, model string, started int64, routeName string, body io.ReadCloser) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		defer body.Close()
		var usage translate.Usage
		completed, failure := false, ""
		finish := func(problem string) {
			if problem == "" && !completed {
				problem = "the image stream ended before the picture was done"
			}
			if problem != "" {
				recordEvent(hub, req, cred.Provider, model, cred.ID, started, "error", usage, "", 0, problem)
				return
			}
			httpx.LogOK(string(cred.Provider), model, routeName)
			record(hub, req, cred.Provider, model, cred.ID, started, "ok", usage, "")
		}
		sc := bufio.NewScanner(body)
		sc.Buffer(make([]byte, 0, 64*1024), maxImageResponse)
		var block strings.Builder
		// An event is its lines up to and including the blank line ending it.
		send := func() bool {
			text := block.String()
			block.Reset()
			if strings.TrimSpace(text) == "" {
				return true
			}
			ev := readImageEvent(text)
			if ev.usage != nil {
				usage = *ev.usage
			}
			if ev.done {
				completed = true
			}
			if ev.failure != "" {
				failure = ev.failure
			}
			select {
			case out <- text:
				return true
			case <-ctx.Done():
				return false
			}
		}
		left := func() { finish("the client left before the picture was done") }
		for sc.Scan() {
			line := sc.Text()
			block.WriteString(line)
			block.WriteByte('\n')
			if line == "" && !send() {
				left()
				return
			}
		}
		if block.Len() > 0 {
			block.WriteByte('\n')
		}
		if !send() {
			left()
			return
		}
		switch {
		case failure != "":
			finish(failure)
		case sc.Err() != nil:
			finish("the image stream stopped before it finished: " + httpx.CleanMessage(sc.Err().Error()))
		default:
			finish("")
		}
	}()
	return out
}

type imageEvent struct {
	usage   *translate.Usage
	done    bool   // a *.completed event, or one that reports usage
	failure string // an error event's message
}

// readImageEvent reads what the meter needs from one server-sent event,
// leaving the picture in it undecoded.
func readImageEvent(event string) imageEvent {
	var ev imageEvent
	for _, line := range strings.Split(event, "\n") {
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		var m struct {
			Type  string         `json:"type"`
			Usage map[string]any `json:"usage"`
			Error any            `json:"error"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(data)), &m) != nil {
			continue
		}
		if u, ok := translate.ParseUsage(m.Usage); ok {
			ev.usage, ev.done = &u, true
		}
		if strings.HasSuffix(m.Type, ".completed") {
			ev.done = true
		}
		if m.Type == "error" || m.Error != nil {
			var whole any
			_ = json.Unmarshal([]byte(strings.TrimSpace(data)), &whole)
			ev.failure = "the image stream failed"
			if msg := httpx.ExtractErrorMessage(whole); msg != "" {
				ev.failure += ": " + msg
			}
		}
	}
	return ev
}
