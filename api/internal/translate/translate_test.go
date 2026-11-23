package translate_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hotelpms/api/internal/translate"
)

func TestLibreTranslateAsksForPlainTextAndReturnsTheTranslation(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/translate" || r.Method != "POST" {
			http.Error(w, "no", 404)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"translatedText":"Un hôtel calme."}`))
	}))
	defer srv.Close()
	tr := translate.NewLibre(srv.URL+"/", "key1", srv.Client())
	out, err := tr.Translate(context.Background(), "A quiet hotel.", "en", "fr")
	if err != nil || out != "Un hôtel calme." {
		t.Fatalf("%q %v", out, err)
	}
	if got["q"] != "A quiet hotel." || got["source"] != "en" || got["target"] != "fr" || got["format"] != "text" || got["api_key"] != "key1" {
		t.Fatalf("%v", got)
	}
	if _, err := tr.Translate(context.Background(), "x", "en", "zh-Hans"); err != nil || got["target"] != "zh" {
		t.Fatalf("Simplified Chinese is asked for as zh: %v %v", err, got["target"])
	}
}

func TestAFailureFromTheServiceIsAnErrorAndNeverAnEmptyTranslation(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, `{"error":"quota"}`, 429) }))
	defer bad.Close()
	if _, err := translate.NewLibre(bad.URL, "", bad.Client()).Translate(context.Background(), "x", "en", "fr"); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("%v", err)
	}
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"translatedText":""}`)) }))
	defer empty.Close()
	if _, err := translate.NewLibre(empty.URL, "", empty.Client()).Translate(context.Background(), "x", "en", "fr"); err == nil {
		t.Fatal("an empty answer was taken as a translation")
	}
}

func TestTheFakeMarksWhatItTranslatedAndCanBeToldToFail(t *testing.T) {
	f := translate.NewFake()
	if out, _ := f.Translate(context.Background(), "Hello", "en", "es"); out != "[es] Hello" {
		t.Fatal(out)
	}
	f.Fail = true
	if _, err := f.Translate(context.Background(), "Hello", "en", "es"); err == nil {
		t.Fatal("no error")
	}
}
