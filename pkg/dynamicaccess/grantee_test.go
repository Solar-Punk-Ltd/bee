package dynamicaccess_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"reflect"
	"testing"

	"github.com/ethersphere/bee/pkg/dynamicaccess"
)

func TestGranteeAddGrantees(t *testing.T) {
	grantee := dynamicaccess.NewGrantee()

	key1, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	key2, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	addList := []ecdsa.PublicKey{key1.PublicKey, key2.PublicKey}
	exampleTopic := "topic"
	grantees, err := grantee.AddGrantees(exampleTopic, addList)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if !reflect.DeepEqual(grantees, addList) {
		t.Errorf("Expected grantees %v, got %v", addList, grantees)
	}
}

func TestRemoveGrantees(t *testing.T) {
	grantee := dynamicaccess.NewGrantee()

	key1, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	key2, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	addList := []ecdsa.PublicKey{key1.PublicKey, key2.PublicKey}
	exampleTopic := "topic"
	grantee.AddGrantees(exampleTopic, addList)

	removeList := []*ecdsa.PublicKey{&key1.PublicKey}
	err = grantee.RemoveGrantees(exampleTopic, removeList)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	grantees := grantee.GetGrantees(exampleTopic)
	expectedGrantees := []ecdsa.PublicKey{key2.PublicKey}

	for i, grantee := range grantees {
		if grantee != expectedGrantees[i] {
			t.Errorf("Expected grantee %v, got %v", expectedGrantees[i], grantee)
		}
	}
}

func TestGetGrantees(t *testing.T) {
	grantee := dynamicaccess.NewGrantee()

	key1, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	key2, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	addList := []ecdsa.PublicKey{key1.PublicKey, key2.PublicKey}
	exampleTopic := "topic"
	grantee.AddGrantees(exampleTopic, addList)

	grantees := grantee.GetGrantees(exampleTopic)
	for i, grantee := range grantees {
		if grantee != addList[i] {
			t.Errorf("Expected grantee %v, got %v", addList[i], grantee)
		}
	}
}
