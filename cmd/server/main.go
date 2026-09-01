// Command server runs whisper-live in headless server mode: WebSocket
// live-streaming transcription plus the plain-HTTP session/export API
// (spec section 8). This is the entrypoint the Docker image (section 10)
// runs, and what a Wails desktop build's Go backend embeds directly.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"

	"whisper-live/internal/adapter/alert"
	"whisper-live/internal/adapter/diarize"
	"whisper-live/internal/adapter/httpapi"
	"whisper-live/internal/adapter/transcriber"
	"whisper-live/internal/adapter/vad"
	"whisper-live/internal/adapter/ws"
	"whisper-live/internal/domain"
	"whisper-live/internal/infra/config"
	"whisper-live/internal/infra/logger"
	"whisper-live/internal/usecase"
)

func main() {
	cfg := config.FromEnv()

	addr := flag.String("addr", cfg.Addr, "HTTP/WS listen address")
	backend := flag.String("transcriber", cfg.TranscriberBackend, "transcriber backend: mock | whispercpp | docker")
	whisperBin := flag.String("whispercpp-bin", cfg.WhisperCppBinary, "path to whisper.cpp CLI binary (backend=whispercpp)")
	whisperModel := flag.String("whispercpp-model", cfg.WhisperCppModel, "path to ggml model file (backend=whispercpp)")
	dockerImage := flag.String("docker-image", cfg.DockerImage, "docker-whisper-tiny image tag (backend=docker)")
	enableVAD := flag.Bool("vad", cfg.EnableVAD, "skip transcription for silent windows")
	enableDiarization := flag.Bool("diarize", cfg.EnableDiarization, "enable lightweight speaker diarization")
	alertKeywords := flag.String("alert-keywords", "", "comma-separated keywords that trigger alerts")
	flag.Parse()

	tr, err := buildTranscriber(*backend, *whisperBin, *whisperModel, *dockerImage)
	if err != nil {
		log.Fatalf("whisper-live: %v", err)
	}
	logger.Infof("transcriber backend: %s", *backend)

	var v usecase.VAD
	if *enableVAD {
		v = vad.NewEnergyVAD()
	}

	var diarizer usecase.Diarizer
	if *enableDiarization {
		diarizer = diarize.NewEmbeddingCluster()
	}

	notifier := alert.NewNotifier()
	var rules []domain.AlertRule
	for i, kw := range splitNonEmpty(*alertKeywords, ",") {
		rules = append(rules, domain.AlertRule{
			ID:      fmt.Sprintf("cli-rule-%d", i),
			Keyword: kw,
			Action:  domain.ActionNotify,
		})
	}

	store := httpapi.NewMemStore()
	api := httpapi.NewAPI(store, nil) // Summarizer wiring left to an operator-supplied backend

	factory := func(sessionID string, tracks []string, language string, sink usecase.SegmentSink) *usecase.SessionRunner {
		return usecase.StartSession(sessionID, tracks, language, usecase.SessionDeps{
			Transcriber: tr,
			VAD:         v,
			Diarizer:    diarizer,
			Sink:        sink,
			Notifier:    notifier,
			AlertRules:  rules,
		})
	}
	wsHandler := ws.NewHandler(factory)
	wsHandler.OnSessionEnd = func(sessionID string, finalSegments []domain.TranscriptSegment) {
		store.Save(sessionID, finalSegments)
		logger.Infof("session %s ended with %d final segments", sessionID, len(finalSegments))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/sessions/", wsHandler.ServeSessionStream)
	mux.HandleFunc("/api/sessions", api.HandleCreateSession)
	mux.HandleFunc("/api/sessions/", routeSessionsAPI(api))
	mux.Handle("/", http.FileServer(http.Dir("frontend/web")))

	logger.Infof("listening on %s", *addr)
	if err := http.ListenAndServe(*addr, withCORS(mux)); err != nil {
		log.Fatalf("whisper-live: server error: %v", err)
	}
}

// routeSessionsAPI dispatches /api/sessions/{id}/transcript.* and
// /api/sessions/{id}/summarize to the right API method.
func routeSessionsAPI(api *httpapi.API) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/summarize") {
			api.HandleSummarize(w, r)
			return
		}
		api.HandleTranscript(w, r)
	}
}

func buildTranscriber(backend, whisperBin, whisperModel, dockerImage string) (usecase.Transcriber, error) {
	switch backend {
	case "", "mock":
		return transcriber.NewMock(), nil
	case "whispercpp":
		if whisperBin == "" || whisperModel == "" {
			return nil, fmt.Errorf("backend=whispercpp requires -whispercpp-bin and -whispercpp-model")
		}
		return transcriber.NewWhisperCppLocal(whisperBin, whisperModel), nil
	case "docker":
		return transcriber.NewWhisperTinyDockerSidecar(dockerImage), nil
	default:
		return nil, fmt.Errorf("unknown transcriber backend %q (want mock | whispercpp | docker)", backend)
	}
}

func splitNonEmpty(s, sep string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, sep) {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
