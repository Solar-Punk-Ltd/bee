package dynamicaccess

import (
	"crypto/ecdsa"
	"encoding/hex"
	"testing"

	"github.com/ethersphere/bee/pkg/crypto"
	"github.com/ethersphere/bee/pkg/swarm"
)

var (
// h *history = NewHistory()
)

func TestDecrypt(t *testing.T) {
	// granteeList := granteeList{grantees: make([]ecdsa.PublicKey, 0)}
	// data, err := hex.DecodeString("c786dd84b61485de12146fd9c4c02d87e8fd95f0542765cb7fc3d2e428c0bcfa")
	// if err != nil {
	// 	t.Fatal(err)
	// }

	// privKey, err := crypto.DecodeSecp256k1PrivateKey(data)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// fmt.Println(crypto.EncodeSecp256k1PrivateKey(privKey))
	// granteeList.Add(privKey.PublicKey)

	swarm.RandAddress(t)
}

type mockAccessLogic struct {
	act Act
}

func (m *mockAccessLogic) GetAccess(encryptedRef swarm.Address, publisher swarm.Address, tag string) (swarm.Address, error) {
	return swarm.Address{}, nil
}

func getPrivateKey() *ecdsa.PrivateKey {
	data, _ := hex.DecodeString("c786dd84b61485de12146fd9c4c02d87e8fd95f0542765cb7fc3d2e428c0bcfa")

	privKey, _ := crypto.DecodeSecp256k1PrivateKey(data)
	return privKey
}
