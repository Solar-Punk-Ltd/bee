package dynamicaccess

import (
	"crypto/ecdsa"
	"fmt"

	encryption "github.com/ethersphere/bee/pkg/encryption"
	"github.com/ethersphere/bee/pkg/kvs"
	"github.com/ethersphere/bee/pkg/swarm"
	"golang.org/x/crypto/sha3"
)

var hashFunc = sha3.NewLegacyKeccak256

// Logic has the responsibility to return a ref for a given grantee and create new encrypted reference for a grantee
type Logic interface {
	// Adds a new grantee to the ACT
	AddNewGranteeToContent(storage kvs.KeyValueStore, publisherPubKey, granteePubKey *ecdsa.PublicKey) error
	// Get will return a decrypted reference, for given encrypted reference and grantee
	Get(storage kvs.KeyValueStore, encryped_ref swarm.Address, publisher *ecdsa.PublicKey) (swarm.Address, error)
}

type ActLogic struct {
	session Session
}

var _ Logic = (*ActLogic)(nil)

// Adds a new publisher to an empty act
func (al ActLogic) AddPublisher(storage kvs.KeyValueStore, publisher *ecdsa.PublicKey) error {
	accessKey := encryption.GenerateRandomKey(encryption.KeyLength)

	keys, err := al.getKeys(publisher)
	if err != nil {
		return err
	}
	lookupKey := keys[0]
	accessKeyEncryptionKey := keys[1]

	accessKeyCipher := encryption.New(encryption.Key(accessKeyEncryptionKey), 0, uint32(0), hashFunc)
	encryptedAccessKey, err := accessKeyCipher.Encrypt([]byte(accessKey))
	if err != nil {
		return err
	}

	return storage.Put(lookupKey, encryptedAccessKey)
}

// Encrypts a SWARM reference for a publisher
func (al ActLogic) EncryptRef(storage kvs.KeyValueStore, publisherPubKey *ecdsa.PublicKey, ref swarm.Address) (swarm.Address, error) {
	accessKey := al.getAccessKey(storage, publisherPubKey)
	refCipher := encryption.New(accessKey, 0, uint32(0), hashFunc)
	encryptedRef, _ := refCipher.Encrypt(ref.Bytes())

	return swarm.NewAddress(encryptedRef), nil
}

// Adds a new grantee to the ACT
func (al ActLogic) AddNewGranteeToContent(storage kvs.KeyValueStore, publisherPubKey, granteePubKey *ecdsa.PublicKey) error {
	// Get previously generated access key
	accessKey := al.getAccessKey(storage, publisherPubKey)

	// Encrypt the access key for the new Grantee
	keys, err := al.getKeys(granteePubKey)
	if err != nil {
		return err
	}
	lookupKey := keys[0]
	accessKeyEncryptionKey := keys[1]

	// Encrypt the access key for the new Grantee
	cipher := encryption.New(encryption.Key(accessKeyEncryptionKey), 0, uint32(0), hashFunc)
	granteeEncryptedAccessKey, err := cipher.Encrypt(accessKey)
	if err != nil {
		return err
	}

	// Add the new encrypted access key for the Act
	return storage.Put(lookupKey, granteeEncryptedAccessKey)

}

// Will return the access key for a publisher (public key)
func (al *ActLogic) getAccessKey(storage kvs.KeyValueStore, publisherPubKey *ecdsa.PublicKey) []byte {
	keys, err := al.getKeys(publisherPubKey)
	if err != nil {
		return nil
	}
	publisherLookupKey := keys[0]
	publisherAKDecryptionKey := keys[1]

	accessKeyDecryptionCipher := encryption.New(encryption.Key(publisherAKDecryptionKey), 0, uint32(0), hashFunc)
	encryptedAK, err := al.getEncryptedAccessKey(storage, publisherLookupKey)
	if err != nil {
		return nil
	}

	accessKey, err := accessKeyDecryptionCipher.Decrypt(encryptedAK)
	if err != nil {
		return nil
	}

	return accessKey
}

func (al *ActLogic) getKeys(publicKey *ecdsa.PublicKey) ([][]byte, error) {
	// Generate lookup key and access key decryption
	oneByteArray := []byte{1}
	zeroByteArray := []byte{0}

	keys, err := al.session.Key(publicKey, [][]byte{zeroByteArray, oneByteArray})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// Gets the encrypted access key for a given grantee
func (al *ActLogic) getEncryptedAccessKey(storage kvs.KeyValueStore, lookup_key []byte) ([]byte, error) {
	val, err := storage.Get(lookup_key)
	if err != nil {
		return nil, err
	}
	return val, nil
}

// Get will return a decrypted reference, for given encrypted reference and grantee
func (al ActLogic) Get(storage kvs.KeyValueStore, encryped_ref swarm.Address, grantee *ecdsa.PublicKey) (swarm.Address, error) {
	if encryped_ref.Compare(swarm.EmptyAddress) == 0 {
		return swarm.EmptyAddress, fmt.Errorf("encrypted ref not provided")
	}
	if grantee == nil {
		return swarm.EmptyAddress, fmt.Errorf("grantee not provided")
	}

	keys, err := al.getKeys(grantee)
	if err != nil {
		return swarm.EmptyAddress, err
	}
	lookupKey := keys[0]
	accessKeyDecryptionKey := keys[1]

	// Lookup encrypted access key from the ACT manifest
	encryptedAccessKey, err := al.getEncryptedAccessKey(storage, lookupKey)
	if err != nil {
		return swarm.EmptyAddress, err
	}

	// Decrypt access key
	accessKeyCipher := encryption.New(encryption.Key(accessKeyDecryptionKey), 0, uint32(0), hashFunc)
	accessKey, err := accessKeyCipher.Decrypt(encryptedAccessKey)
	if err != nil {
		return swarm.EmptyAddress, err
	}

	// Decrypt reference
	refCipher := encryption.New(accessKey, 0, uint32(0), hashFunc)
	ref, err := refCipher.Decrypt(encryped_ref.Bytes())
	if err != nil {
		return swarm.EmptyAddress, err
	}

	return swarm.NewAddress(ref), nil
}

func NewLogic(s Session) ActLogic {
	return ActLogic{
		session: s,
	}
}
