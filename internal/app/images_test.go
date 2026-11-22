package app

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/kacperkwapisz/mainspring/internal/httpx"
)

func TestImageFormBecomesTheJSONBody(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", "gpt-image-2")
	_ = mw.WriteField("prompt", "add a hat")
	_ = mw.WriteField("n", "2")
	_ = mw.WriteField("stream", "false")
	png := []byte("\x89PNG\r\n\x1a\n rest")
	for _, name := range []string{"image[]", "image[]"} {
		w, _ := mw.CreateFormFile(name, "fox.png") // sent as application/octet-stream
		_, _ = w.Write(png)
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="mask"; filename="mask.webp"`)
	h.Set("Content-Type", "image/webp")
	w, _ := mw.CreatePart(h)
	_, _ = w.Write([]byte("RIFF"))
	_ = mw.Close()

	r := httptest.NewRequest("POST", "/v1/images/edits", &buf)
	r.Header.Set("content-type", mw.FormDataContentType())
	body, err := imageBody(r)
	if err != nil {
		t.Fatal(err)
	}
	if body["model"] != "gpt-image-2" || body["prompt"] != "add a hat" || body["n"] != 2.0 || body["stream"] != false {
		t.Fatalf("fields: %v", body)
	}
	images, _ := body["images"].([]any)
	if len(images) != 2 {
		t.Fatalf("images: %v", body["images"])
	}
	url, _ := images[0].(map[string]any)["image_url"].(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("an untyped picture should be named from its bytes: %.40s", url)
	}
	mask, _ := body["mask"].(map[string]any)["image_url"].(string)
	if mask != "data:image/webp;base64,UklGRg==" {
		t.Fatalf("mask: %v", body["mask"])
	}
}

func TestImageBodyTakesJSON(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(`{"model":"gpt-image-2","prompt":"a fox"}`))
	r.Header.Set("content-type", "application/json")
	body, err := imageBody(r)
	if err != nil || body["prompt"] != "a fox" {
		t.Fatalf("%v %v", body, err)
	}
}

func TestImageFormRefusesWhatItCantSend(t *testing.T) {
	form := func(fill func(*multipart.Writer)) (map[string]any, error) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fill(mw)
		_ = mw.Close()
		r := httptest.NewRequest("POST", "/v1/images/edits", &buf)
		r.Header.Set("content-type", mw.FormDataContentType())
		return imageBody(r)
	}
	if _, err := form(func(mw *multipart.Writer) {
		w, _ := mw.CreateFormFile("photo", "x.png")
		_, _ = w.Write([]byte("x"))
	}); err == nil || !strings.Contains(err.Error(), "photo") {
		t.Fatalf("an unknown file field should be refused: %v", err)
	}
	if _, err := form(func(mw *multipart.Writer) { _ = mw.WriteField("n", "two") }); err == nil {
		t.Fatal("n must be a number")
	}
	big := strings.Repeat("a", httpx.BodyLimitBytes+10)
	_, err := form(func(mw *multipart.Writer) {
		w, _ := mw.CreateFormFile("image", "big.png")
		_, _ = w.Write([]byte(big))
	})
	var se *httpx.StatusError
	if !errors.As(err, &se) || se.Status != 413 {
		t.Fatalf("want 413, got %v", err)
	}
	r := httptest.NewRequest("POST", "/v1/images/edits", strings.NewReader("x"))
	r.Header.Set("content-type", "multipart/form-data")
	if _, err := imageBody(r); err == nil {
		t.Fatal("a form without a boundary can't be read")
	}
}

func TestImageRoutesNeedAKey(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	created, _ := booted.Hub.Keys.Create("t", nil, nil, nil)
	for _, path := range []string{"/v1/images/generations", "/v1/images/edits"} {
		res := httptest.NewRecorder()
		booted.Handler.ServeHTTP(res, httptest.NewRequest("POST", path, strings.NewReader(`{"model":"gpt-image-2","prompt":"x"}`)))
		if res.Code != 401 {
			t.Fatalf("%s without a key: %d", path, res.Code)
		}
		// With a key and no Codex account, the answer says so.
		req := httptest.NewRequest("POST", path, strings.NewReader(`{"model":"gpt-image-2","prompt":"x"}`))
		req.Header.Set("authorization", "Bearer "+created.Secret)
		req.Header.Set("content-type", "application/json")
		res = httptest.NewRecorder()
		booted.Handler.ServeHTTP(res, req)
		if res.Code != 503 || !strings.Contains(res.Body.String(), "Codex") {
			t.Fatalf("%s with no Codex account: %d %s", path, res.Code, res.Body.String())
		}
	}
}
