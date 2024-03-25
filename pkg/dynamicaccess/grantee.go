package dynamicaccess

import (
	"crypto/ecdsa"
)

type Grantee interface {
	Add(topic string, addList []*ecdsa.PublicKey) error
	Remove(topic string, removeList []*ecdsa.PublicKey) error
	Get(topic string) []*ecdsa.PublicKey
}

var _ Grantee = (*defaultGrantee)(nil)

type defaultGrantee struct {
	grantees map[string][]*ecdsa.PublicKey
}

func (g *defaultGrantee) Get(topic string) []*ecdsa.PublicKey {
	grantees := g.grantees[topic]
	keys := make([]*ecdsa.PublicKey, len(grantees))
	copy(keys, grantees)
	return keys
}

func (g *defaultGrantee) Add(topic string, addList []*ecdsa.PublicKey) error {
	g.grantees[topic] = append(g.grantees[topic], addList...)
	return nil
}

func (g *defaultGrantee) Remove(topic string, removeList []*ecdsa.PublicKey) error {
	for _, remove := range removeList {
		for i, grantee := range g.grantees[topic] {
			if *grantee == *remove {
				g.grantees[topic][i] = g.grantees[topic][len(g.grantees[topic])-1]
				g.grantees[topic] = g.grantees[topic][:len(g.grantees[topic])-1]
			}
		}
	}

	return nil
}

func NewGrantee() Grantee {
	return &defaultGrantee{grantees: make(map[string][]*ecdsa.PublicKey)}
}
