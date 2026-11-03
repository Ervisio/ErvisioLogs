package archive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Provider types stored in storage_providers.type.
const (
	TypeS3    = "s3"
	TypeSFTP  = "sftp"
	TypeLocal = "local"
)

// secretNameRe matches the secret_file values the database accepts: a bare file name inside the
// secrets directory, never a path.
var secretNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

// LocalConfig is the config of a local provider.
type LocalConfig struct {
	Root string `json:"root"`
}

// Open builds a provider from its stored type, JSON config and secret file name. Secrets are read
// from <secretsDir>/<secretFile> and never stored in the database (ADR 0008).
func Open(typ string, config json.RawMessage, secretFile, secretsDir string) (Provider, error) {
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}
	readSecret := func() ([]byte, error) {
		if secretFile == "" {
			return nil, errors.New("this provider needs a secret file")
		}
		if !ValidSecretFile(secretFile) {
			return nil, errors.New("secret_file must be a plain file name")
		}
		if secretsDir == "" {
			return nil, errors.New("no secrets directory is configured (ERVISIO_LOGS_ARCHIVE_SECRETS_DIR)")
		}
		b, err := os.ReadFile(filepath.Join(secretsDir, secretFile))
		if err != nil {
			// The path is configuration, not a secret; the content never appears in errors.
			return nil, fmt.Errorf("cannot read secret file %s: %w", secretFile, errors.Unwrap(err))
		}
		return b, nil
	}
	switch typ {
	case TypeS3:
		var cfg S3Config
		if err := strictJSON(config, &cfg); err != nil {
			return nil, err
		}
		sec, err := readSecret()
		if err != nil {
			return nil, err
		}
		return NewS3(cfg, sec)
	case TypeSFTP:
		var cfg SFTPConfig
		if err := strictJSON(config, &cfg); err != nil {
			return nil, err
		}
		sec, err := readSecret()
		if err != nil {
			return nil, err
		}
		return NewSFTP(cfg, sec)
	case TypeLocal:
		var cfg LocalConfig
		if err := strictJSON(config, &cfg); err != nil {
			return nil, err
		}
		return NewLocalFS(cfg.Root)
	default:
		return nil, fmt.Errorf("unknown provider type %q", typ)
	}
}

// ValidSecretFile reports whether name is a plain file name inside the secrets directory.
func ValidSecretFile(name string) bool {
	return secretNameRe.MatchString(name) && name != "." && name != ".."
}

// strictJSON decodes config into v, rejecting unknown fields so a typo ("bucketname") fails at
// save time instead of silently using a default.
func strictJSON(config json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(config))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("provider config: %w", err)
	}
	return nil
}
