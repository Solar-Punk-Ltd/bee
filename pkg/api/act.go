package api

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ethersphere/bee/pkg/encryption"
	"github.com/ethersphere/bee/pkg/feeds"
	"github.com/ethersphere/bee/pkg/manifest/simple"
	"github.com/ethersphere/bee/pkg/sctx"
	"golang.org/x/crypto/scrypt"
	cli "gopkg.in/urfave/cli.v1"
)

var (
	ErrDecrypt                = errors.New("cant decrypt - forbidden")
	ErrUnknownAccessType      = errors.New("unknown access type (or not implemented)")
	ErrDecryptDomainForbidden = errors.New("decryption request domain forbidden - can only decrypt on localhost")
	AllowedDecryptDomains     = []string{
		"localhost",
		"127.0.0.1",
	}
)

const EmptyCredentials = ""

// ManifestEntry represents an entry in a swarm manifest
type ManifestEntry struct {
	Hash        string       `json:"hash,omitempty"`
	Path        string       `json:"path,omitempty"`
	ContentType string       `json:"contentType,omitempty"`
	Mode        int64        `json:"mode,omitempty"`
	Size        int64        `json:"size,omitempty"`
	ModTime     time.Time    `json:"mod_time,omitempty"`
	Status      int          `json:"status,omitempty"`
	Access      *AccessEntry `json:"access,omitempty"`
	Feed        *feeds.Feed  `json:"feed,omitempty"`
}

type AccessEntry struct {
	Type      AccessType
	Publisher string
	Salt      []byte
	Act       string
	KdfParams *KdfParams
}

type DecryptFunc func(*ManifestEntry) error

type Marshaler interface {
	Marshal() ([]byte, error)
}

type Unmarshaler interface {
	Unmarshal([]byte) error
}

// TODO marshal and unmarshal

type KdfParams struct {
	N int `json:"n"`
	P int `json:"p"`
	R int `json:"r"`
}

type AccessType string

const AccessTypePass = AccessType("pass")

// NewAccessEntryPassword creates a manifest AccessEntry in order to create an ACT protected by a password
func NewAccessEntryPassword(salt []byte, kdfParams *KdfParams) (*AccessEntry, error) {
	if len(salt) != 32 {
		return nil, fmt.Errorf("salt should be 32 bytes long")
	}
	return &AccessEntry{
		Type:      AccessTypePass,
		Salt:      salt,
		KdfParams: kdfParams,
	}, nil
}

// NOOPDecrypt is a generic decrypt function that is passed into the API in places where real ACT decryption capabilities are
// either unwanted, or alternatively, cannot be implemented in the immediate scope
func NOOPDecrypt(*ManifestEntry) error {
	return nil
}

var DefaultKdfParams = NewKdfParams(262144, 1, 8)

// NewKdfParams returns a KdfParams struct with the given scrypt params
func NewKdfParams(n, p, r int) *KdfParams {

	return &KdfParams{
		N: n,
		P: p,
		R: r,
	}
}

// NewSessionKeyPassword creates a session key based on a shared secret (password) and the given salt
// and kdf parameters in the access entry
func NewSessionKeyPassword(password string, accessEntry *AccessEntry) ([]byte, error) {
	if accessEntry.Type != AccessTypePass {
		return nil, errors.New("incorrect access entry type")

	}
	return sessionKeyPassword(password, accessEntry.Salt, accessEntry.KdfParams)
}

func sessionKeyPassword(password string, salt []byte, kdfParams *KdfParams) ([]byte, error) {
	return scrypt.Key(
		[]byte(password),
		salt,
		kdfParams.N,
		kdfParams.R,
		kdfParams.P,
		32,
	)
}

func (a *Service) show(ctx context.Context, credentials string, pk *ecdsa.PrivateKey) DecryptFunc {
	return func(m *ManifestEntry) error {
		if m.Access == nil {
			return nil
		}

		allowed := false
		requestDomain := sctx.GetHost(ctx)
		for _, v := range AllowedDecryptDomains {
			if strings.Contains(requestDomain, v) {
				allowed = true
			}
		}

		if !allowed {
			return ErrDecryptDomainForbidden
		}

		switch m.Access.Type {
		case "pass":
			if credentials != "" {
				key, err := NewSessionKeyPassword(credentials, m.Access)
				if err != nil {
					return err
				}

				ref, err := hex.DecodeString(m.Hash)
				if err != nil {
					return err
				}

				//enc := encryption.Encrypt(len(ref) - 8)
				// TODO nil!
				decodedRef, err := encryption.New(key, 0, 0, nil).Decrypt(ref)
				if err != nil {
					return ErrDecrypt
				}

				m.Hash = hex.EncodeToString(decodedRef)
				m.Access = nil
				return nil
			}
			return ErrDecrypt
		}
		return ErrUnknownAccessType
	}
}

func GenerateAccessControlManifest(ctx *cli.Context, ref string, accessKey []byte, ae *AccessEntry) (*simple.Manifest, error) {
	refBytes, err := hex.DecodeString(ref)
	if err != nil {
		return nil, err
	}
	// encrypt ref with accessKey
	// TODO nil!
	encrypted, err := encryption.New(accessKey, 0, 0, nil).Encrypt(refBytes)
	// TODO _ = encrypted
	_ = encrypted
	if err != nil {
		return nil, err
	}
	vManifest := simple.NewManifest()
	// TODO Add
	//vManifest.Add()
	/*
		m := &simple.manifest{
			Entries: []ManifestEntry{
				{
					Hash:        hex.EncodeToString(encrypted),
					ContentType: manifest.DefaultManifestType,
					ModTime:     time.Now(),
					Access:      ae,
				},
			},
		}
	*/

	return &vManifest, nil
}

// DoPassword is a helper function to the CLI API that handles the entire business logic for
// creating a session key and an access entry given the cli context, password and salt.
// By default - DefaultKdfParams are used as the scrypt params
func DoPassword(ctx *cli.Context, password string, salt []byte) (sessionKey []byte, ae *AccessEntry, err error) {
	ae, err = NewAccessEntryPassword(salt, DefaultKdfParams)
	if err != nil {
		return nil, nil, err
	}

	sessionKey, err = NewSessionKeyPassword(password, ae)
	if err != nil {
		return nil, nil, err
	}
	return sessionKey, ae, nil
}
