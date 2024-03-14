package dynamicaccess

import (
	"crypto/ecdsa"

	"github.com/ethersphere/bee/pkg/swarm"
)

type Controller interface {
	DownloadHandler(timestamp int64, enryptedRef swarm.Address, publisher *ecdsa.PublicKey, tag string) (swarm.Address, error)
}

type defaultController struct {
	history        History
	granteeManager GranteeManager
	accessLogic    AccessLogic
}

func (c *defaultController) DownloadHandler(timestamp int64, enryptedRef swarm.Address, publisher *ecdsa.PublicKey, tag string) (swarm.Address, error) {
	act, err := c.history.Lookup(timestamp)
	if err != nil {
		return swarm.EmptyAddress, err
	}
	addr, err := c.accessLogic.Get(act, enryptedRef, *publisher, tag)
	return addr, err
}

func NewController(history History, granteeManager GranteeManager, accessLogic AccessLogic) Controller {
	return &defaultController{
		history:        history,
		granteeManager: granteeManager,
		accessLogic:    accessLogic,
	}
}
