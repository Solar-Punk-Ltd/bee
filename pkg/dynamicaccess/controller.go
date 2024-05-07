package dynamicaccess

import (
	"context"
	"crypto/ecdsa"
	"io"
	"time"

	encryption "github.com/ethersphere/bee/v2/pkg/encryption"
	"github.com/ethersphere/bee/v2/pkg/file"
	"github.com/ethersphere/bee/v2/pkg/file/pipeline"
	"github.com/ethersphere/bee/v2/pkg/file/pipeline/builder"
	"github.com/ethersphere/bee/v2/pkg/file/redundancy"
	"github.com/ethersphere/bee/v2/pkg/kvs"
	"github.com/ethersphere/bee/v2/pkg/storage"
	"github.com/ethersphere/bee/v2/pkg/swarm"
)

type GranteeManager interface {
	// TODO: doc
	HandleGrantees(ctx context.Context, ls file.LoadSaver, gls file.LoadSaver, granteeref swarm.Address, historyref swarm.Address, publisher *ecdsa.PublicKey, addList, removeList []*ecdsa.PublicKey) (swarm.Address, swarm.Address, swarm.Address, swarm.Address, error)
	// GetGrantees returns the list of grantees for the given publisher.
	// The list is accessible only by the publisher.
	GetGrantees(ctx context.Context, ls file.LoadSaver, publisher *ecdsa.PublicKey, encryptedglref swarm.Address) ([]*ecdsa.PublicKey, error)
}

type Controller interface {
	GranteeManager
	// DownloadHandler decrypts the encryptedRef using the lookupkey based on the history and timestamp.
	DownloadHandler(ctx context.Context, ls file.LoadSaver, encryptedRef swarm.Address, publisher *ecdsa.PublicKey, historyRootHash swarm.Address, timestamp int64) (swarm.Address, error)
	// UploadHandler encrypts the reference and stores it in the history as the latest update.
	UploadHandler(ctx context.Context, ls file.LoadSaver, reference swarm.Address, publisher *ecdsa.PublicKey, historyRootHash swarm.Address) (swarm.Address, swarm.Address, swarm.Address, error)
	io.Closer
}

type controller struct {
	accessLogic ActLogic
}

var _ Controller = (*controller)(nil)

func (c *controller) DownloadHandler(
	ctx context.Context,
	ls file.LoadSaver,
	encryptedRef swarm.Address,
	publisher *ecdsa.PublicKey,
	historyRootHash swarm.Address,
	timestamp int64,
) (swarm.Address, error) {
	history, err := NewHistoryReference(ls, historyRootHash)
	if err != nil {
		return swarm.ZeroAddress, err
	}
	entry, err := history.Lookup(ctx, timestamp)
	if err != nil {
		return swarm.ZeroAddress, err
	}
	act, err := kvs.NewReference(ls, entry.Reference())
	if err != nil {
		return swarm.ZeroAddress, err
	}

	return c.accessLogic.DecryptRef(ctx, act, encryptedRef, publisher)
}

func (c *controller) UploadHandler(
	ctx context.Context,
	ls file.LoadSaver,
	refrefence swarm.Address,
	publisher *ecdsa.PublicKey,
	historyRootHash swarm.Address,
) (swarm.Address, swarm.Address, swarm.Address, error) {
	historyRef := historyRootHash
	var (
		storage kvs.KeyValueStore
		actRef  swarm.Address
	)
	now := time.Now().Unix()
	if historyRef.IsZero() {
		history, err := NewHistory(ls)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
		storage, err = kvs.New(ls)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
		err = c.accessLogic.AddPublisher(ctx, storage, publisher)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
		actRef, err = storage.Save(ctx)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
		err = history.Add(ctx, actRef, &now, nil)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
		historyRef, err = history.Store(ctx)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
	} else {
		history, err := NewHistoryReference(ls, historyRef)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
		entry, err := history.Lookup(ctx, now)
		actRef = entry.Reference()
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
		storage, err = kvs.NewReference(ls, actRef)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
	}

	encryptedRef, err := c.accessLogic.EncryptRef(ctx, storage, publisher, refrefence)
	return actRef, historyRef, encryptedRef, err
}

func NewController(accessLogic ActLogic) Controller {
	return &controller{
		accessLogic: accessLogic,
	}
}

func (c *controller) HandleGrantees(
	ctx context.Context,
	ls file.LoadSaver,
	gls file.LoadSaver,
	encryptedglref swarm.Address,
	historyref swarm.Address,
	publisher *ecdsa.PublicKey,
	addList []*ecdsa.PublicKey,
	removeList []*ecdsa.PublicKey,
) (swarm.Address, swarm.Address, swarm.Address, swarm.Address, error) {
	var (
		err        error
		h          History
		act        kvs.KeyValueStore
		granteeref swarm.Address
	)
	if !historyref.IsZero() {
		h, err = NewHistoryReference(ls, historyref)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
		entry, err := h.Lookup(ctx, time.Now().Unix())
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
		actref := entry.Reference()
		act, err = kvs.NewReference(ls, actref)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
	} else {
		h, err = NewHistory(ls)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
		act, err = kvs.New(ls)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
	}

	var gl GranteeList
	if encryptedglref.IsZero() {
		gl, err = NewGranteeList(gls)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
	} else {
		granteeref, err = c.decryptRefForPublisher(publisher, encryptedglref)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}

		gl, err = NewGranteeListReference(gls, granteeref)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
	}
	err = gl.Add(addList)
	if err != nil {
		return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
	}
	if len(removeList) != 0 {
		err = gl.Remove(removeList)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
	}

	var granteesToAdd []*ecdsa.PublicKey
	// generate new access key and new act
	if len(removeList) != 0 || encryptedglref.IsZero() {
		if historyref.IsZero() {
			err = c.accessLogic.AddPublisher(ctx, act, publisher)
			if err != nil {
				return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
			}
		}
		granteesToAdd = gl.Get()
	} else {
		granteesToAdd = addList
	}

	for _, grantee := range granteesToAdd {
		err := c.accessLogic.AddGrantee(ctx, act, publisher, grantee, nil)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
	}

	actref, err := act.Save(ctx)
	if err != nil {
		return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	glref, err := gl.Save(ctx)
	if err != nil {
		return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	eglref, err := c.encryptRefForPublisher(publisher, glref)
	if err != nil {
		return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
	}
	// need to re-initialize history, because Lookup loads the forks causing the manifest save to skip the root node
	if !historyref.IsZero() {
		h, err = NewHistoryReference(ls, historyref)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
		}
	}

	mtdt := map[string]string{"encryptedglref": eglref.String()}
	err = h.Add(ctx, actref, nil, &mtdt)
	if err != nil {
		return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
	}
	href, err := h.Store(ctx)
	if err != nil {
		return swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	return glref, eglref, href, actref, nil
}

func (c *controller) GetGrantees(ctx context.Context, ls file.LoadSaver, publisher *ecdsa.PublicKey, encryptedglref swarm.Address) ([]*ecdsa.PublicKey, error) {
	granteeRef, err := c.decryptRefForPublisher(publisher, encryptedglref)
	if err != nil {
		return nil, err
	}
	gl, err := NewGranteeListReference(ls, granteeRef)
	if err != nil {
		return nil, err
	}
	return gl.Get(), nil
}

func (c *controller) encryptRefForPublisher(publisherPubKey *ecdsa.PublicKey, ref swarm.Address) (swarm.Address, error) {
	keys, err := c.accessLogic.Session.Key(publisherPubKey, [][]byte{oneByteArray})
	if err != nil {
		return swarm.ZeroAddress, err
	}
	refCipher := encryption.New(keys[0], 0, uint32(0), hashFunc)
	encryptedRef, err := refCipher.Encrypt(ref.Bytes())
	if err != nil {
		return swarm.ZeroAddress, err
	}

	return swarm.NewAddress(encryptedRef), nil
}

func (c *controller) decryptRefForPublisher(publisherPubKey *ecdsa.PublicKey, encryptedRef swarm.Address) (swarm.Address, error) {
	keys, err := c.accessLogic.Session.Key(publisherPubKey, [][]byte{oneByteArray})
	if err != nil {
		return swarm.ZeroAddress, err
	}
	refCipher := encryption.New(keys[0], 0, uint32(0), hashFunc)
	ref, err := refCipher.Decrypt(encryptedRef.Bytes())
	if err != nil {
		return swarm.ZeroAddress, err
	}

	return swarm.NewAddress(ref), nil
}

func requestPipelineFactory(ctx context.Context, s storage.Putter, encrypt bool, rLevel redundancy.Level) func() pipeline.Interface {
	return func() pipeline.Interface {
		return builder.NewPipelineBuilder(ctx, s, encrypt, rLevel)
	}
}

// TODO: what to do in close ?
func (s *controller) Close() error {
	return nil
}
