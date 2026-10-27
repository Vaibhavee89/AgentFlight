package api

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Vaibhavee89/agentflight/internal/benchmark"
	"github.com/Vaibhavee89/agentflight/internal/replay"
)

// replayResponse is the outcome of POST /runs/{id}/replay. MismatchAtSeq is
// set only when replay diverged at a specific recorded event.
type replayResponse struct {
	OK            bool   `json:"ok"`
	MismatchAtSeq *int64 `json:"mismatch_at_seq"`
	Detail        string `json:"detail"`
}

// handleReplay re-executes a finished run against its recording. A matching
// replay returns ok=true. A divergence returns ok=false with the seq of the
// first recorded event that disagreed. A run that cannot be replayed at all,
// such as one still executing or one whose log is incomplete, returns 422.
func (s *Server) handleReplay(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	found, err := s.exists(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if s.runs.active(id) {
		writeError(w, http.StatusConflict, "run is still active; replay a finished run")
		return
	}

	_, err = replay.Replay(r.Context(), s.cfg.Store, id)
	if err == nil {
		writeJSON(w, http.StatusOK, replayResponse{OK: true, Detail: "replay reproduced the recorded result"})
		return
	}

	var mm *replay.MismatchError
	if errors.As(err, &mm) {
		resp := replayResponse{OK: false, Detail: mm.Error()}
		if mm.Seq != 0 {
			seq := mm.Seq
			resp.MismatchAtSeq = &seq
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	writeJSON(w, http.StatusUnprocessableEntity, replayResponse{OK: false, Detail: err.Error()})
}

// handleBenchResults serves the per-execution results written by
// `harnessbench run` from BenchDir/results.json, unchanged.
func (s *Server) handleBenchResults(w http.ResponseWriter, r *http.Request) {
	s.serveBenchFile(w, "results.json", "harnessbench run -out results.json")
}

// handleBenchCompare serves the comparison written by `harnessbench compare
// --json` from BenchDir/compare.json, after checking its schema version.
func (s *Server) handleBenchCompare(w http.ResponseWriter, r *http.Request) {
	if s.cfg.BenchDir == "" {
		writeError(w, http.StatusNotFound, "no bench directory configured; start serve with --bench-dir")
		return
	}
	doc, err := benchmark.ReadCompareDocument(filepath.Join(s.cfg.BenchDir, "compare.json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeError(w, http.StatusNotFound, "compare.json not found in "+s.cfg.BenchDir+"; run harnessbench compare --json compare.json")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) serveBenchFile(w http.ResponseWriter, name, hint string) {
	if s.cfg.BenchDir == "" {
		writeError(w, http.StatusNotFound, "no bench directory configured; start serve with --bench-dir")
		return
	}
	path := filepath.Join(s.cfg.BenchDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeError(w, http.StatusNotFound, name+" not found in "+s.cfg.BenchDir+"; run "+hint)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
