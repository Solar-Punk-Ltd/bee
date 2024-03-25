package dynamicaccess

import (
	"crypto/ecdsa"

	encryption "github.com/ethersphere/bee/pkg/encryption"
	"github.com/ethersphere/bee/pkg/swarm"
	"golang.org/x/crypto/sha3"
)

var hashFunc = sha3.NewLegacyKeccak256

// Read-only interface for the ACT
type Decryptor interface {
	// DecryptRef will return a decrypted reference, for given encrypted reference and grantee
	DecryptRef(rootHash swarm.Address, encryped_ref swarm.Address, publisher *ecdsa.PublicKey) (swarm.Address, error)
	Session
}

// Control interface for the ACT (does write operations)
type Control interface {
	Decryptor
	// Adds a new grantee to the ACT
	AddGrantee(rootHash swarm.Address, publisherPubKey, granteePubKey *ecdsa.PublicKey, accessKey *encryption.Key) (swarm.Address, error)
	// Encrypts a Swarm reference for a given grantee
	EncryptRef(rootHash swarm.Address, grantee *ecdsa.PublicKey, ref swarm.Address) (swarm.Address, error)
}

type ActLogic struct {
	Session
	act Act
}

var _ Decryptor = (*ActLogic)(nil)

// Adds a new publisher to an empty act
func (al ActLogic) AddPublisher(rootHash swarm.Address, publisher *ecdsa.PublicKey) (swarm.Address, error) {
	accessKey := encryption.GenerateRandomKey(encryption.KeyLength)

	return al.AddGrantee(rootHash, publisher, publisher, &accessKey)
}

// Encrypts a SWARM reference for a publisher
func (al ActLogic) EncryptRef(rootHash swarm.Address, publisherPubKey *ecdsa.PublicKey, ref swarm.Address) (swarm.Address, error) {
	accessKey, err := al.getAccessKey(rootHash, publisherPubKey)
	if err != nil {
		return swarm.EmptyAddress, err
	}
	refCipher := encryption.New(accessKey, 0, uint32(0), hashFunc)
	encryptedRef, _ := refCipher.Encrypt(ref.Bytes())

	return swarm.NewAddress(encryptedRef), nil
}

// Adds a new grantee to the ACT
func (al ActLogic) AddGrantee(rootHash swarm.Address, publisherPubKey, granteePubKey *ecdsa.PublicKey, accessKeyPointer *encryption.Key) (swarm.Address, error) {
	var accessKey encryption.Key
	var err error // Declare the "err" variable

	if accessKeyPointer == nil {
		// Get previously generated access key
		accessKey, err = al.getAccessKey(rootHash, publisherPubKey)
		if err != nil {
			return swarm.EmptyAddress, err
		}
	} else {
		// This is a newly created access key, because grantee is publisher (they are the same)
		accessKey = *accessKeyPointer
	}

	// Encrypt the access key for the new Grantee
	keys, err := al.getKeys(granteePubKey)
	if err != nil {
		return swarm.EmptyAddress, err
	}
	lookupKey := keys[0]
	accessKeyEncryptionKey := keys[1]

	// Encrypt the access key for the new Grantee
	cipher := encryption.New(encryption.Key(accessKeyEncryptionKey), 0, uint32(0), hashFunc)
	granteeEncryptedAccessKey, err := cipher.Encrypt(accessKey)
	if err != nil {
		return swarm.EmptyAddress, err
	}

	// Add the new encrypted access key for the Act
	return al.act.Add(rootHash, lookupKey, granteeEncryptedAccessKey)
}

// Will return the access key for a publisher (public key)
func (al *ActLogic) getAccessKey(rootHash swarm.Address, publisherPubKey *ecdsa.PublicKey) ([]byte, error) {
	keys, err := al.getKeys(publisherPubKey)
	if err != nil {
		return nil, err
	}
	publisherLookupKey := keys[0]
	publisherAKDecryptionKey := keys[1]
	// no need to constructor call if value not found in act
	accessKeyDecryptionCipher := encryption.New(encryption.Key(publisherAKDecryptionKey), 0, uint32(0), hashFunc)
	encryptedAK, err := al.act.Lookup(rootHash, publisherLookupKey)
	if err != nil {
		return nil, err
	}

	return accessKeyDecryptionCipher.Decrypt(encryptedAK)

}

var oneByteArray = []byte{1}
var zeroByteArray = []byte{0}

// Generate lookup key and access key decryption key for a given public key
func (al *ActLogic) getKeys(publicKey *ecdsa.PublicKey) ([][]byte, error) {
	return al.Session.Key(publicKey, [][]byte{zeroByteArray, oneByteArray})
}

// DecryptRef will return a decrypted reference, for given encrypted reference and grantee
func (al ActLogic) DecryptRef(rootHash swarm.Address, encryped_ref swarm.Address, grantee *ecdsa.PublicKey) (swarm.Address, error) {
	keys, err := al.getKeys(grantee)
	if err != nil {
		return swarm.EmptyAddress, err
	}
	lookupKey := keys[0]
	accessKeyDecryptionKey := keys[1]

	// Lookup encrypted access key from the ACT manifest
	encryptedAccessKey, err := al.act.Lookup(rootHash, lookupKey)
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

func NewLogic(S Session, act Act) ActLogic {
	return ActLogic{
		Session: S,
		act:     act,
	}
}
