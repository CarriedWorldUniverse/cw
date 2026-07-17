package cred

import (
	"crypto/sha256"
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
	localSlug    = "cw-satchel-local-v1"
	localKeyInfo = "cw-satchel-secret-v1"
	repoIdentity = "cw-satchel"
)

type FileStore struct {
	dir string
}

func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

func (s *FileStore) Put(name string, plaintext []byte, passphrase string) error {
	if err := validateSecretName(name); err != nil {
		return err
	}
	key, err := secretKey(passphrase)
	if err != nil {
		return err
	}
	blob, err := casket.Seal(key, plaintext, casket.SealOptions{
		KeyType:      casket.KeyTypeDerivedRepo,
		RepoIdentity: []byte(repoIdentity),
		ObjectPath:   []byte(name),
	})
	if err != nil {
		return err
	}
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
	if _, err := tmp.Write(blob); err != nil {
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
	key, err := secretKey(passphrase)
	if err != nil {
		return nil, err
	}
	plaintext, _, err := casket.Open(key, b, []byte(repoIdentity), []byte(name))
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

func (s *FileStore) Delete(name string) error {
	if err := validateSecretName(name); err != nil {
		return err
	}
	err := os.Remove(s.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	return err
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

func secretKey(passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("cred: empty passphrase")
	}
	priv, _, err := casket.DeriveAgentKey([]byte(passphrase), localSlug)
	if err != nil {
		return nil, err
	}
	r := hkdf.New(sha256.New, priv.Seed(), nil, []byte(localKeyInfo))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}
