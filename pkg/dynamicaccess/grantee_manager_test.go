package dynamicaccess_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"testing"

	"github.com/ethersphere/bee/pkg/crypto"
	"github.com/ethersphere/bee/pkg/dynamicaccess"
)

func setupAccessLogic() dynamicaccess.AccessLogic {
	privateKey, err := crypto.GenerateSecp256k1Key()
	if err != nil {
		errors.New("error creating private key")
	}
	diffieHellman := dynamicaccess.NewDiffieHellman(privateKey)
	al := dynamicaccess.NewAccessLogic(diffieHellman)

	return al
}

func TestGet(t *testing.T) {
	act := dynamicaccess.NewDefaultAct()
	m := dynamicaccess.NewManager(setupAccessLogic())
	pub, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	id1, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	id2, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	m.Add("topic", []*ecdsa.PublicKey{&id1.PublicKey})
	m.Add("topic", []*ecdsa.PublicKey{&id2.PublicKey})
	m.Publish(act, pub.PublicKey, "topic")
	fmt.Println("")
}
