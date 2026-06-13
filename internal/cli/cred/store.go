package cred

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	casket "github.com/CarriedWorldUniverse/casket-go"
	"golang.org/x/crypto/hkdf"
)

var (
	ErrNotFound = errors.New("cred: secret not found")
	ErrDecrypt  = errors.New("cred: decrypt failed")
)

const (
	envelopeVersion = 1
	saltSize        = 16
	nonceSize       = 12
	localSlug       = "cw-satchel-local-v1"
	localKeyInfo    = "cw-satchel-secret-v1:"
)

type FileStore struct {
	dir string
}

type envelope struct {
	Version    int    `json:"v"`
	KDF        string `json:"kdf"`
	CasketSlug string `json:"casket_slug"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

func (s *FileStore) Put(name string, plaintext []byte, passphrase string) error {
	if err := validateSecretName(name); err != nil {
		return err
	}
	salt, err := randomBytes(saltSize)
	if err != nil {
		return err
	}
	nonce, err := randomBytes(nonceSize)
	if err != nil {
		return err
	}
	key, err := secretKey(passphrase, salt, name)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	env := envelope{
		Version:    envelopeVersion,
		KDF:        "casket.DeriveAgentKey+HKDF-SHA256",
		CasketSlug: localSlug,
		Salt:       b64(salt),
		Nonce:      b64(nonce),
	}
	env.Ciphertext = b64(gcm.Seal(nil, nonce, plaintext, aad(name, env)))
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	path := s.path(name)
	tmp, err := os.CreateTemp(s.dir, "."+name+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}

func (s *FileStore) Get(name string, passphrase string) ([]byte, error) {
	if err := validateSecretName(name); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(s.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("%w: malformed envelope", ErrDecrypt)
	}
	if env.Version != envelopeVersion || env.CasketSlug != localSlug {
		return nil, fmt.Errorf("%w: unsupported envelope", ErrDecrypt)
	}
	salt, err := unb64(env.Salt)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed salt", ErrDecrypt)
	}
	nonce, err := unb64(env.Nonce)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed nonce", ErrDecrypt)
	}
	ciphertext, err := unb64(env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed ciphertext", ErrDecrypt)
	}
	key, err := secretKey(passphrase, salt, name)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad(name, env))
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

func (s *FileStore) List() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".casket.json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".casket.json")
		if validateSecretName(name) == nil {
			names = append(names, name)
		}
	}
	return sortedNames(names), nil
}

func (s *FileStore) path(name string) string {
	return filepath.Join(s.dir, name+".casket.json")
}

func secretKey(passphrase string, salt []byte, name string) ([]byte, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("cred: empty passphrase")
	}
	priv, _, err := casket.DeriveAgentKey([]byte(passphrase), localSlug)
	if err != nil {
		return nil, err
	}
	r := hkdf.New(sha256.New, priv.Seed(), salt, []byte(localKeyInfo+name))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}

func aad(name string, env envelope) []byte {
	return []byte(fmt.Sprintf("cw-cred:%d:%s:%s:%s", env.Version, personalNamespace, name, env.CasketSlug))
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func b64(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func unb64(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
