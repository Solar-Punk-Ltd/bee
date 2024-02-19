package api

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ethersphere/bee/pkg/encryption"
	"github.com/ethersphere/bee/pkg/feeds"
	"github.com/ethersphere/bee/pkg/manifest/simple"
	"github.com/ethersphere/bee/pkg/sctx"
	"golang.org/x/crypto/scrypt"
	"golang.org/x/crypto/sha3"
	cli "gopkg.in/urfave/cli.v1"
)

type Act interface {
	upload(publisher string, salt []byte) (*AccessEntry, error)
	NewAccessEntryPassword(salt []byte, kdfParams *KdfParams) (*AccessEntry, error)
	NOOPDecrypt(*ManifestEntry) error
	NewKdfParams(n, p, r int) *KdfParams
	NewSessionKeyPassword(password string, accessEntry *AccessEntry) ([]byte, error)
	create(ctx *cli.Context, ref string, accessKey []byte, ae *AccessEntry) (*simple.Manifest, error)
	DoPassword(ctx *cli.Context, password string, salt []byte) (sessionKey []byte, ae *AccessEntry, err error)
}

var (
	ErrSaltLength             = errors.New("salt should be 32 bytes long")
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
	Hash        string
	Path        string
	ContentType string
	Mode        int64
	Size        int64
	ModTime     time.Time
	Status      int
	Access      *AccessEntry
	Feed        *feeds.Feed
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
	Unmarshal(value []byte) error
}

// Marshal returns the JSON encoding of the AccessEntry.
// It marshals the AccessEntry struct into a byte slice using the json.Marshal function.
// If an error occurs during marshaling, it returns the error.
func (a *AccessEntry) Marshal() ([]byte, error) {
	return json.Marshal(a)
}

// Unmarshal unmarshals the given byte slice into the AccessEntry struct.
// It uses JSON unmarshaling to populate the fields of the struct.
// If the unmarshaling fails, it returns an error.
// After unmarshaling, it checks if the length of the Salt field is 32 bytes.
// If not, it returns an error indicating that the salt should be 32 bytes long.
// Finally, it decodes the Salt field from hexadecimal string to byte slice.
// If the decoding fails, it returns an error.
func (a *AccessEntry) Unmarshal(value []byte) error {
	err := json.Unmarshal(value, a)
	if err != nil {
		return err
	}
	if !saltLengthIs32(a.Salt) {
		return ErrSaltLength
	}
	a.Salt, err = hex.DecodeString(string(a.Salt))
	return err
}

type KdfParams struct {
	N int `json:"n"`
	P int `json:"p"`
	R int `json:"r"`
}

type AccessType int

const (
	AccessTypePass AccessType = iota
	AccessTypePK
	AccessTypeACT
)

func saltLengthIs32(salt []byte) bool {
	return len(salt) == 32
}

// upload creates a manifest AccessEntry in order to create an ACT protected by a pair of Elliptic Curve keys
func upload(publisher string, salt []byte) (*AccessEntry, error) {
	if len(publisher) != 66 {
		return nil, fmt.Errorf("publisher should be 66 characters long, got %d", len(publisher))
	}
	if !saltLengthIs32(salt) {
		return nil, ErrSaltLength
	}
	return &AccessEntry{
		Type:      AccessTypePK,
		Publisher: publisher,
		Salt:      salt,
	}, nil
}

// NewAccessEntryPassword creates a manifest AccessEntry in order to create an ACT protected by a password
func NewAccessEntryPassword(salt []byte, kdfParams *KdfParams) (*AccessEntry, error) {
	if !saltLengthIs32(salt) {
		return nil, ErrSaltLength
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

func (a *Service) Show(ctx context.Context, credentials string, pk *ecdsa.PrivateKey) DecryptFunc {
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
		case AccessTypePass:
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
				decodedRef, err := encryption.New(key, len(ref)-8, uint32(0), sha3.NewLegacyKeccak256).Decrypt(ref)
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

func Create(ctx *cli.Context, ref string, accessKey []byte, ae *AccessEntry) (*simple.Manifest, error) {
	refBytes, err := hex.DecodeString(ref)
	if err != nil {
		return nil, err
	}
	// encrypt ref with accessKey
	encrypted, err := encryption.New(accessKey, len(ref)-8, uint32(0), sha3.NewLegacyKeccak256).Encrypt(refBytes)
	// TODO _ = encrypted
	_ = encrypted
	if err != nil {
		return nil, err
	}
	vManifest := simple.NewManifest()
	// TODO Add
	/*
		keyMap := make(map[string]string)

		keyMap["Hash"] = hex.EncodeToString(encrypted)
		keyMap["ContentType"] = manifest.DefaultManifestType
		keyMap["ModTime"] = time.Now().String()
		keyMap["Access"] = ae.Act // TODO ???
		vManifest := simple.NewManifest()
		vManifest.Add("", "", keyMap)
	*/
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
