package dynamicaccess_test

import (
	"crypto/ecdsa"
	"encoding/hex"
	"testing"
	"time"

	"github.com/ethersphere/bee/pkg/crypto"
	"github.com/ethersphere/bee/pkg/dynamicaccess"
	"github.com/ethersphere/bee/pkg/dynamicaccess/mock"
	"github.com/ethersphere/bee/pkg/encryption"
	"github.com/ethersphere/bee/pkg/swarm"
	"golang.org/x/crypto/sha3"
)

var hashFunc = sha3.NewLegacyKeccak256

func mockTestHistory(key, val []byte) dynamicaccess.History {
	var (
		h   = mock.NewHistory()
		now = time.Now()
		act = mock.NewActMock(nil, func(lookupKey []byte) []byte {
			return val
		})
	)
	// act.Add(key, val)
	h.Insert(now.AddDate(-3, 0, 0).Unix(), act)
	return h
}

func TestDecrypt(t *testing.T) {
	pk := getPrivateKey()
	ak := encryption.Key("cica")
	e1 := encryption.New(ak, 0, uint32(0), hashFunc)
	dh := dynamicaccess.NewDiffieHellman(pk)
	aek, _ := dh.SharedSecret(&pk.PublicKey, "", []byte("1"))
	e2 := encryption.New(aek, 0, uint32(0), hashFunc)
	peak, _ := e2.Encrypt([]byte("cica"))

	h := mockTestHistory(nil, peak)
	al := setupAccessLogic(pk)
	gm := dynamicaccess.NewGranteeManager(al)
	c := dynamicaccess.NewController(h, gm, al)
	ch := prepareChunkReference()
	// ech := al.EncryptRef(ch, "tag")
	ech, err := e1.Encrypt(ch.Bytes())
	if err != nil {
		t.Fatalf("Encrypt() returned an error: %v", err)
	}

	ts := int64(0)
	addr, err := c.DownloadHandler(ts, swarm.NewAddress(ech), &pk.PublicKey, "tag")
	if err != nil {
		t.Fatalf("DownloadHandler() returned an error: %v", err)
	}
	if addr.String() != ch.String() {
		t.Fatalf("Decrypted chunk address: %s is not the expected: %s", addr.String(), ch.String())
	}
}

func prepareChunkReference() swarm.Address {
	addr, _ := hex.DecodeString("f7b1a45b70ee91d3dbfd98a2a692387f24db7279a9c96c447409e9205cf265baef29bf6aa294264762e33f6a18318562c86383dd8bfea2cec14fae08a8039bf3")
	return swarm.NewAddress(addr)
}

func getPrivateKey() *ecdsa.PrivateKey {
	data, _ := hex.DecodeString("c786dd84b61485de12146fd9c4c02d87e8fd95f0542765cb7fc3d2e428c0bcfa")

	privKey, _ := crypto.DecodeSecp256k1PrivateKey(data)
	return privKey
}
