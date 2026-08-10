package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/models"
	"fan-web/services"
)

var fakePNG = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52}
var fakeGIF = []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00")

func setupCoverHandler(t *testing.T) *AnimeHandler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbPath := filepath.Join(t.TempDir(), "cover.db")
	if err := database.Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})
	return NewAnimeHandler(services.NewBangumiService(), services.NewScannerService(t.TempDir()))
}

// trustedTransport 把 lain.bgm.tv 的请求转到本地 origin，模拟信任主机。
type trustedTransport struct {
	origin string
}

func (rt *trustedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rewritten := req.Clone(req.Context())
	parsed, _ := url.Parse(rt.origin + req.URL.Path)
	rewritten.URL = parsed
	return http.DefaultTransport.RoundTrip(rewritten)
}

func coverClientFor(origin string) *http.Client {
	client := newCoverClient()
	client.Transport = &trustedTransport{origin: origin}
	return client
}

func serveCover(t *testing.T, handler *AnimeHandler, animeID int64) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	router.GET("/api/animes/:id/cover", handler.Cover)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/animes/"+int64ToString(animeID)+"/cover", nil)
	router.ServeHTTP(recorder, req)
	return recorder
}

func int64ToString(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestCoverAcceptsTrustedPNG(t *testing.T) {
	handler := setupCoverHandler(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fakePNG)
	}))
	defer origin.Close()

	anime, err := database.CreateAnime(&models.Anime{Title: "Covered", Cover: "https://lain.bgm.tv/pic/cover/l/a1.png"})
	if err != nil {
		t.Fatal(err)
	}
	handler.coverClient = coverClientFor(origin.URL)

	recorder := serveCover(t, handler, anime.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected nosniff, got %q", got)
	}
}

func TestCoverRejectsUntrustedHost(t *testing.T) {
	handler := setupCoverHandler(t)
	anime, err := database.CreateAnime(&models.Anime{Title: "Evil", Cover: "https://evil.example.com/x.png"})
	if err != nil {
		t.Fatal(err)
	}
	recorder := serveCover(t, handler, anime.ID)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("untrusted host should be 404, got %d", recorder.Code)
	}
}

func TestCoverRedirectToUntrustedBlocked(t *testing.T) {
	untrusted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("untrusted redirect target must not receive request")
	}))
	defer untrusted.Close()

	trusted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, untrusted.URL+"/steal.png", http.StatusFound)
	}))
	defer trusted.Close()

	handler := setupCoverHandler(t)
	anime, err := database.CreateAnime(&models.Anime{Title: "Redirect", Cover: "https://lain.bgm.tv/redirect"})
	if err != nil {
		t.Fatal(err)
	}
	handler.coverClient = coverClientFor(trusted.URL)

	recorder := serveCover(t, handler, anime.ID)
	if recorder.Code == http.StatusOK {
		t.Fatalf("redirect to untrusted host must not succeed, got %d", recorder.Code)
	}
}

func TestCoverRejectsOversizeKnownLength(t *testing.T) {
	handler := setupCoverHandler(t)
	// 超大响应的伪造 content-length。
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", "109051904") // > 10 MiB
		_, _ = w.Write(make([]byte, 100))
	}))
	defer origin.Close()

	anime, err := database.CreateAnime(&models.Anime{Title: "Big", Cover: "https://lain.bgm.tv/big.png"})
	if err != nil {
		t.Fatal(err)
	}
	handler.coverClient = coverClientFor(origin.URL)

	recorder := serveCover(t, handler, anime.ID)
	if recorder.Code == http.StatusOK {
		t.Fatalf("oversize cover must be rejected, got %d", recorder.Code)
	}
}

func TestCoverRejectsSVGOrWrongContentType(t *testing.T) {
	handler := setupCoverHandler(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte("<svg/>"))
	}))
	defer origin.Close()

	anime, err := database.CreateAnime(&models.Anime{Title: "Svg", Cover: "https://lain.bgm.tv/x.svg"})
	if err != nil {
		t.Fatal(err)
	}
	handler.coverClient = coverClientFor(origin.URL)

	recorder := serveCover(t, handler, anime.ID)
	if recorder.Code == http.StatusOK {
		t.Fatalf("image/svg+xml must be rejected, got %d", recorder.Code)
	}
}

func TestCoverRejectsTooManyRedirects(t *testing.T) {
	handler := setupCoverHandler(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/r0":
			http.Redirect(w, r, "https://lain.bgm.tv/r1", http.StatusFound)
		case "/r1":
			http.Redirect(w, r, "https://lain.bgm.tv/r2", http.StatusFound)
		case "/r2":
			http.Redirect(w, r, "https://lain.bgm.tv/r3", http.StatusFound)
		case "/r3":
			http.Redirect(w, r, "https://lain.bgm.tv/r4", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(fakePNG)
		}
	}))
	defer origin.Close()

	anime, err := database.CreateAnime(&models.Anime{Title: "Redirect Loop", Cover: "https://lain.bgm.tv/r0"})
	if err != nil {
		t.Fatal(err)
	}
	handler.coverClient = coverClientFor(origin.URL)
	if recorder := serveCover(t, handler, anime.ID); recorder.Code == http.StatusOK {
		t.Fatal("more than three redirects must be rejected")
	}
}

func TestCoverRejectsOversizeUnknownLength(t *testing.T) {
	handler := setupCoverHandler(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = w.Write(fakePNG)
		_, _ = w.Write(bytes.Repeat([]byte{0}, maxCoverBytes+1-len(fakePNG)))
	}))
	defer origin.Close()

	anime, err := database.CreateAnime(&models.Anime{Title: "Chunked Big", Cover: "https://lain.bgm.tv/chunked.png"})
	if err != nil {
		t.Fatal(err)
	}
	handler.coverClient = coverClientFor(origin.URL)
	if recorder := serveCover(t, handler, anime.ID); recorder.Code == http.StatusOK {
		t.Fatal("unknown-length oversized cover must be rejected")
	}
}

func TestCoverRejectsDeclaredAndDetectedTypeMismatch(t *testing.T) {
	handler := setupCoverHandler(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fakeGIF)
	}))
	defer origin.Close()

	anime, err := database.CreateAnime(&models.Anime{Title: "Mismatched", Cover: "https://lain.bgm.tv/mismatch.png"})
	if err != nil {
		t.Fatal(err)
	}
	handler.coverClient = coverClientFor(origin.URL)
	if recorder := serveCover(t, handler, anime.ID); recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content-type mismatch must be 415, got %d", recorder.Code)
	}
}
