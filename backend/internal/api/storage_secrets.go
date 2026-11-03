package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Ervisio/ErvisioLogs/backend/internal/archive"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
)

// Secret files of archive destinations, written from the Storage page (ADR 0017). The API stores a
// value it receives and never sends one back: listing returns names only.

// maxSecretBytes bounds a secret file; an S3 key pair or an SSH private key is far smaller.
const maxSecretBytes = 32 << 10

func (a *API) secretsDir(w http.ResponseWriter, r *http.Request) (string, bool) {
	if a.Storage == nil || a.Storage.SecretsDir == "" {
		httpx.WriteProblem(w, r, http.StatusConflict, "no secrets directory is configured (ERVISIO_LOGS_ARCHIVE_SECRETS_DIR)")
		return "", false
	}
	return a.Storage.SecretsDir, true
}

func (a *API) adminListSecrets(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	dir, ok := a.secretsDir(w, r)
	if !ok {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	names := []string{}
	for _, e := range entries {
		if e.Type().IsRegular() && !strings.HasPrefix(e.Name(), ".") && archive.ValidSecretFile(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	items(w, names)
}

func (a *API) adminPutSecret(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	name := chi.URLParam(r, "name")
	if !archive.ValidSecretFile(name) || strings.HasPrefix(name, ".") {
		invalid(w, r, "name: letters, digits, dot, dash or underscore, up to 100 characters, not starting with a dot")
		return
	}
	var req struct {
		Value string `json:"value"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Value == "" || len(req.Value) > maxSecretBytes || hasNUL(req.Value) {
		invalid(w, r, "value must be 1 byte to 32 KiB of text")
		return
	}
	dir, ok := a.secretsDir(w, r)
	if !ok {
		return
	}
	replaced := true
	if _, err := os.Lstat(filepath.Join(dir, name)); errors.Is(err, os.ErrNotExist) {
		replaced = false
	}
	if err := writeSecretFile(dir, name, []byte(req.Value)); err != nil {
		// The error names the path, never the value.
		a.internalError(w, r, err)
		return
	}
	a.audit(r, nil, "storage.secret.write", "storage_secret", name, map[string]any{"replaced": replaced})
	w.WriteHeader(http.StatusNoContent)
}

// writeSecretFile replaces dir/name in one rename, so a reader sees the old file or the new one.
// The file is readable by the service user only.
func writeSecretFile(dir, name string, value []byte) error {
	tmp, err := os.CreateTemp(dir, ".incoming-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(value); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}
