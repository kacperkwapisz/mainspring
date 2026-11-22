package app

import (
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
)

const inboundImages = string(proxy.InboundImages)

func registerImages(mux *httpx.Mux, hub *proxy.Hub) {
	handle := func(edit bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			key := requireLocalKey(r, hub)
			if key == nil {
				writeUnauthorized(w, r, inboundImages)
				return
			}
			payload, err := imageBody(r)
			if err != nil {
				writeParseError(w, r, inboundImages, err)
				return
			}
			result, err := proxy.Images(r.Context(), hub, *key, httpx.InboundHeaders(r), edit, payload)
			if err != nil {
				writeProxyErr(w, r, inboundImages, err)
				return
			}
			writeProxy(w, r, result)
		}
	}
	mux.Handle(http.MethodPost, "/v1/images/generations", handle(false))
	mux.Handle(http.MethodPost, "/v1/images/edits", handle(true))
}

// imageBody reads an Images API request: JSON, or the multipart form OpenAI's
// SDKs send for edits.
func imageBody(r *http.Request) (map[string]any, error) {
	mediaType, params, _ := mime.ParseMediaType(r.Header.Get("content-type"))
	if mediaType == "multipart/form-data" {
		return imageForm(r.Body, params["boundary"])
	}
	body, err := httpx.ParseBody(r)
	if err != nil {
		return nil, err
	}
	payload, ok := body.(map[string]any)
	if !ok {
		return nil, &httpx.StatusError{Status: 400, Message: "send the request as a JSON object or a multipart form"}
	}
	return payload, nil
}

// Form fields that the JSON body carries as numbers or booleans.
var (
	imageNumberFields = map[string]bool{"n": true, "output_compression": true, "partial_images": true, "frame_count": true, "frame_duration_ms": true}
	imageBoolFields   = map[string]bool{"stream": true}
)

// imageForm turns a multipart Images request into the JSON body the Images
// API also takes: each image file becomes {"image_url": "data:…"} in
// "images", and a mask file {"image_url": …} in "mask".
func imageForm(body io.Reader, boundary string) (map[string]any, error) {
	if boundary == "" {
		return nil, &httpx.StatusError{Status: 400, Message: "the multipart form has no boundary"}
	}
	counted := &countingReader{r: io.LimitReader(body, httpx.BodyLimitBytes+1)}
	reader := multipart.NewReader(counted, boundary)
	unreadable := func(err error) error {
		if counted.n > httpx.BodyLimitBytes {
			return &httpx.StatusError{Status: 413, Message: "payload too large"}
		}
		return &httpx.StatusError{Status: 400, Message: "the multipart form can't be read: " + err.Error()}
	}
	out := map[string]any{}
	var images []any
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, unreadable(err)
		}
		name := strings.TrimSuffix(part.FormName(), "[]")
		raw, err := io.ReadAll(part)
		part.Close()
		if err != nil {
			return nil, unreadable(err)
		}
		if part.FileName() != "" {
			ref := map[string]any{"image_url": dataURL(part.Header.Get("content-type"), raw)}
			switch name {
			case "image":
				images = append(images, ref)
			case "mask":
				out["mask"] = ref
			default:
				return nil, &httpx.StatusError{Status: 400, Message: "unexpected file field " + strconv.Quote(name) + "; send pictures as image or image[], and a mask as mask"}
			}
			continue
		}
		value := string(raw)
		switch {
		case imageNumberFields[name]:
			n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				return nil, &httpx.StatusError{Status: 400, Message: name + " must be a number"}
			}
			out[name] = n
		case imageBoolFields[name]:
			b, err := strconv.ParseBool(strings.TrimSpace(value))
			if err != nil {
				return nil, &httpx.StatusError{Status: 400, Message: name + " must be true or false"}
			}
			out[name] = b
		default:
			out[name] = value
		}
	}
	if len(images) > 0 {
		out["images"] = images
	}
	return out, nil
}

// dataURL inlines a file as a data: URL. A part with no type, or a generic
// one, is named from its bytes.
func dataURL(contentType string, raw []byte) string {
	mt, _, _ := mime.ParseMediaType(contentType)
	if mt == "" || mt == "application/octet-stream" {
		mt = http.DetectContentType(raw)
	}
	return "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(raw)
}

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}
