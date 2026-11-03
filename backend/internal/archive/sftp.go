package archive

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SFTPConfig holds the non-secret settings of an SFTP provider such as a Hetzner Storage Box.
type SFTPConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
	// BaseDir is where objects live, relative to the login directory unless it starts with '/'.
	BaseDir string `json:"base_dir"`
	// HostKey is the server's public key in authorized_keys form ("ssh-ed25519 AAAA...").
	// Connections to a server with any other key are refused.
	HostKey string `json:"host_key"`
	// Auth says what the secret file holds: "key" (default) an unencrypted private key, "password"
	// the account password. Hetzner Storage Boxes accept both.
	Auth string `json:"auth,omitempty"`
}

// SFTP auth modes.
const (
	SFTPAuthKey      = "key"
	SFTPAuthPassword = "password"
)

// SFTP is a provider over SSH. It assumes nothing beyond plain files: no server checksums and no
// ranged reads, so verification reads every upload back (STORAGE_AND_RETENTION.md).
type SFTP struct {
	cfg    SFTPConfig
	ssh    *ssh.ClientConfig
	addr   string
	base   string
	mu     sync.Mutex
	conn   *ssh.Client
	client *sftp.Client
}

// NewSFTP takes the secret file's content: a private key (PEM, OpenSSH format) or, with Auth
// "password", the password (one trailing newline is dropped).
func NewSFTP(cfg SFTPConfig, secret []byte) (*SFTP, error) {
	if cfg.Host == "" || cfg.User == "" {
		return nil, errors.New("sftp host and user are required")
	}
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, errors.New("sftp port is out of range")
	}
	hostKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(cfg.HostKey))
	if err != nil {
		return nil, errors.New("sftp host_key must be a public key line such as \"ssh-ed25519 AAAA...\"")
	}
	var auth []ssh.AuthMethod
	switch cfg.Auth {
	case "", SFTPAuthKey:
		signer, err := ssh.ParsePrivateKey(secret)
		if err != nil {
			return nil, errors.New("sftp secret file must hold an unencrypted private key (or set auth to \"password\")")
		}
		auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	case SFTPAuthPassword:
		pw := strings.TrimSuffix(strings.TrimSuffix(string(secret), "\n"), "\r")
		if pw == "" {
			return nil, errors.New("sftp secret file is empty")
		}
		// Some servers offer only keyboard-interactive; answer every prompt with the password.
		answer := func(_, _ string, questions []string, _ []bool) ([]string, error) {
			out := make([]string, len(questions))
			for i := range out {
				out[i] = pw
			}
			return out, nil
		}
		auth = []ssh.AuthMethod{ssh.Password(pw), ssh.KeyboardInteractive(answer)}
	default:
		return nil, errors.New("sftp auth must be \"key\" or \"password\"")
	}
	base := strings.TrimRight(cfg.BaseDir, "/")
	if base == "" {
		base = "."
	}
	for _, seg := range strings.Split(strings.TrimPrefix(base, "/"), "/") {
		if seg == ".." {
			return nil, errors.New("sftp base_dir may not contain '..'")
		}
	}
	return &SFTP{
		cfg:  cfg,
		addr: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		base: base,
		ssh: &ssh.ClientConfig{
			User:            cfg.User,
			Auth:            auth,
			HostKeyCallback: ssh.FixedHostKey(hostKey),
			// Ask only for the pinned key's algorithm; otherwise the server may present another of
			// its host keys and the pin fails.
			HostKeyAlgorithms: hostKeyAlgorithms(hostKey.Type()),
			Timeout:           15 * time.Second,
		},
	}, nil
}

func hostKeyAlgorithms(keyType string) []string {
	if keyType == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}
	return []string{keyType}
}

func (s *SFTP) path(key string) (string, error) {
	if err := ValidKey(key); err != nil {
		return "", err
	}
	return path.Join(s.base, key), nil
}

// session returns a live client, dialing if needed.
func (s *SFTP) session(ctx context.Context) (*sftp.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return s.client, nil
	}
	d := net.Dialer{Timeout: s.ssh.Timeout}
	raw, err := d.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(raw, s.addr, s.ssh)
	if err != nil {
		raw.Close()
		return nil, err
	}
	conn := ssh.NewClient(c, chans, reqs)
	client, err := sftp.NewClient(conn, sftp.UseConcurrentWrites(true))
	if err != nil {
		conn.Close()
		return nil, err
	}
	s.conn, s.client = conn, client
	return client, nil
}

// do runs fn on a session. A broken connection is dropped and fn retried once on a new one.
func (s *SFTP) do(ctx context.Context, fn func(*sftp.Client) error) error {
	for attempt := 0; ; attempt++ {
		c, err := s.session(ctx)
		if err != nil {
			return err
		}
		err = fn(c)
		if err == nil || attempt > 0 || !connLost(err) || ctx.Err() != nil {
			return err
		}
		s.reset(c)
	}
}

func connLost(err error) bool {
	return errors.Is(err, sftp.ErrSSHFxConnectionLost) || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed)
}

func (s *SFTP) reset(c *sftp.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == c {
		s.client.Close()
		s.conn.Close()
		s.client, s.conn = nil, nil
	}
}

func (s *SFTP) mkdirs(c *sftp.Client, dir string) error {
	if err := c.MkdirAll(dir); err != nil {
		if st, serr := c.Stat(dir); serr == nil && st.IsDir() {
			return nil
		}
		return err
	}
	return nil
}

// Put writes to a hidden ".part" file next to the key, checks the size the server reports, then
// renames it into place, so an interrupted transfer never leaves a file under the key.
func (s *SFTP) Put(ctx context.Context, key string, r io.Reader, size int64, _ []byte) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	part := path.Join(path.Dir(p), "."+path.Base(p)+".part-"+newObjectName())
	// No retry inside: the reader cannot be rewound. The caller retries the whole upload.
	c, err := s.session(ctx)
	if err != nil {
		return err
	}
	err = func() error {
		if err := s.mkdirs(c, path.Dir(p)); err != nil {
			return err
		}
		f, err := c.OpenFile(part, osCreateTrunc)
		if err != nil {
			return err
		}
		n, err := io.Copy(f, &ctxReader{ctx: ctx, r: r})
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		if n != size {
			return fmt.Errorf("short upload: wrote %d of %d bytes", n, size)
		}
		st, err := c.Stat(part)
		if err != nil {
			return err
		}
		if st.Size() != size {
			return fmt.Errorf("short upload: server has %d of %d bytes", st.Size(), size)
		}
		return s.rename(c, part, p)
	}()
	if err != nil {
		if connLost(err) {
			s.reset(c)
		} else {
			_ = c.Remove(part)
		}
	}
	return err
}

// rename overwrites the target when the server has the OpenSSH posix-rename extension; plain
// SFTP rename fails on an existing target.
func (s *SFTP) rename(c *sftp.Client, from, to string) error {
	if _, ok := c.HasExtension("posix-rename@openssh.com"); ok {
		return c.PosixRename(from, to)
	}
	return c.Rename(from, to)
}

func (s *SFTP) Promote(ctx context.Context, tmpKey, finalKey string) error {
	src, err := s.path(tmpKey)
	if err != nil {
		return err
	}
	dst, err := s.path(finalKey)
	if err != nil {
		return err
	}
	return s.do(ctx, func(c *sftp.Client) error {
		if err := s.mkdirs(c, path.Dir(dst)); err != nil {
			return err
		}
		if err := s.rename(c, src, dst); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// A retry after a rename that did go through finds the file already in place.
				if _, serr := c.Stat(dst); serr == nil {
					return nil
				}
				return ErrNotExist
			}
			return err
		}
		return nil
	})
}

func (s *SFTP) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, err
	}
	var f *sftp.File
	err = s.do(ctx, func(c *sftp.Client) error {
		var err error
		f, err = c.Open(p)
		return err
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotExist
	}
	return f, err
}

func (s *SFTP) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	p, err := s.path(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	var info ObjectInfo
	err = s.do(ctx, func(c *sftp.Client) error {
		st, err := c.Stat(p)
		if err != nil {
			return err
		}
		info = ObjectInfo{Key: key, Size: st.Size(), ModTime: st.ModTime()}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return ObjectInfo{}, ErrNotExist
	}
	return info, err
}

// List walks the base directory. ".part" leftovers are reported too, so orphan cleanup sees them.
func (s *SFTP) List(ctx context.Context, prefix string, fn func(ObjectInfo) error) error {
	return s.do(ctx, func(c *sftp.Client) error {
		w := c.Walk(s.base)
		for w.Step() {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := w.Err(); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			st := w.Stat()
			if st.IsDir() {
				continue
			}
			key := strings.TrimPrefix(strings.TrimPrefix(w.Path(), s.base), "/")
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			if err := fn(ObjectInfo{Key: key, Size: st.Size(), ModTime: st.ModTime()}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SFTP) Delete(ctx context.Context, key string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	return s.do(ctx, func(c *sftp.Client) error {
		if err := c.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	})
}

// DeleteRaw removes a listed path that is not a valid key, such as a ".part" leftover.
func (s *SFTP) DeleteRaw(ctx context.Context, listed string) error {
	if strings.Contains(listed, "..") || strings.HasPrefix(listed, "/") {
		return fmt.Errorf("refusing to delete %q", listed)
	}
	return s.do(ctx, func(c *sftp.Client) error {
		if err := c.Remove(path.Join(s.base, listed)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	})
}

// Health checks the login, the base directory and that it is writable.
func (s *SFTP) Health(ctx context.Context) error {
	return s.do(ctx, func(c *sftp.Client) error {
		if err := s.mkdirs(c, s.base); err != nil {
			return fmt.Errorf("cannot create %s: %w", s.base, err)
		}
		p := path.Join(s.base, ".health-"+newObjectName())
		f, err := c.OpenFile(p, osCreateTrunc)
		if err != nil {
			return fmt.Errorf("cannot write to %s: %w", s.base, err)
		}
		f.Close()
		return c.Remove(p)
	})
}

func (s *SFTP) Capabilities() Capabilities {
	return Capabilities{AtomicRename: true}
}

func (s *SFTP) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		s.client.Close()
		s.conn.Close()
		s.client, s.conn = nil, nil
	}
	return nil
}

func newObjectName() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// pkg/sftp translates os flags into SFTP flags itself, so the host OS values are the right ones.
const osCreateTrunc = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
