// Copyright 2022 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pinstore

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"

	"github.com/ethersphere/bee/v2/pkg/encryption"
	storage "github.com/ethersphere/bee/v2/pkg/storage"
	"github.com/ethersphere/bee/v2/pkg/storer/internal/transaction"
	"golang.org/x/sync/errgroup"

	"github.com/ethersphere/bee/v2/pkg/storage/storageutil"
	"github.com/ethersphere/bee/v2/pkg/storer/internal"
	"github.com/ethersphere/bee/v2/pkg/swarm"
	"github.com/google/uuid"
)

const (
	// size of the UUID generated by the pinstore
	uuidSize = 16
)

var (
	// errInvalidPinCollectionAddr is returned when trying to marshal a pinCollectionItem
	// with a zero address
	errInvalidPinCollectionAddr = errors.New("marshal pinCollectionItem: address is zero")
	// errInvalidPinCollectionUUID is returned when trying to marshal a pinCollectionItem
	// with an empty UUID
	errInvalidPinCollectionUUID = errors.New("marshal pinCollectionItem: UUID is zero")
	// errInvalidPinCollectionSize is returned when trying to unmarshal a buffer of
	// incorrect size
	errInvalidPinCollectionSize = errors.New("unmarshal pinCollectionItem: invalid size")
	// errPutterAlreadyClosed is returned when trying to use a Putter which is already closed
	errPutterAlreadyClosed = errors.New("pin store: putter already closed")
	// errCollectionRootAddressIsZero is returned if the putter is closed with a zero
	// swarm.Address. Root reference has to be set.
	errCollectionRootAddressIsZero = errors.New("pin store: collection root address is zero")
	// ErrDuplicatePinCollection is returned when attempted to pin the same file repeatedly
	ErrDuplicatePinCollection = errors.New("pin store: duplicate pin collection")
)

// creates a new UUID and returns it as a byte slice
func newUUID() []byte {
	id := uuid.New()
	return id[:]
}

// emptyKey is a 32 byte slice of zeros used to check if encryption key is set
var emptyKey = make([]byte, 32)

// CollectionStat is used to store some basic stats about the pinning collection
type CollectionStat struct {
	Total           uint64
	DupInCollection uint64
}

// NewCollection returns a putter wrapped around the passed storage.
// The putter will add the chunk to Chunk store if it doesnt exists within this collection.
// It will create a new UUID for the collection which can be used to iterate on all the chunks
// that are part of this collection. The root pin is only updated on successful close of this.
// Calls to the Putter MUST be mutex locked to prevent concurrent upload data races.
func NewCollection(st storage.IndexStore) (internal.PutterCloserWithReference, error) {
	newCollectionUUID := newUUID()
	err := st.Put(&dirtyCollection{UUID: newCollectionUUID})
	if err != nil {
		return nil, err
	}
	return &collectionPutter{
		collection: &pinCollectionItem{UUID: newCollectionUUID},
	}, nil
}

type collectionPutter struct {
	collection *pinCollectionItem
	closed     bool
}

// Put adds a chunk to the pin collection.
// The user of the putter MUST mutex lock the call to prevent data-races across multiple upload sessions.
func (c *collectionPutter) Put(ctx context.Context, st transaction.Store, ch swarm.Chunk) error {

	// do not allow any Puts after putter was closed
	if c.closed {
		return errPutterAlreadyClosed
	}

	c.collection.Stat.Total++

	// We will only care about duplicates within this collection. In order to
	// guarantee that we dont accidentally delete common chunks across collections,
	// a separate pinCollectionItem entry will be present for each duplicate chunk.
	collectionChunk := &pinChunkItem{UUID: c.collection.UUID, Addr: ch.Address()}
	found, err := st.IndexStore().Has(collectionChunk)
	if err != nil {
		return fmt.Errorf("pin store: failed to check chunk: %w", err)
	}
	if found {
		// If we already have this chunk in the current collection, don't add it
		// again.
		c.collection.Stat.DupInCollection++
		return nil
	}

	err = st.IndexStore().Put(collectionChunk)
	if err != nil {
		return fmt.Errorf("pin store: failed putting collection chunk: %w", err)
	}

	err = st.ChunkStore().Put(ctx, ch)
	if err != nil {
		return fmt.Errorf("pin store: failled putting chunk: %w", err)
	}

	return nil
}

func (c *collectionPutter) Close(st storage.IndexStore, root swarm.Address) error {
	if root.IsZero() {
		return errCollectionRootAddressIsZero
	}

	collection := &pinCollectionItem{Addr: root}
	has, err := st.Has(collection)

	if err != nil {
		return fmt.Errorf("pin store: check previous root: %w", err)
	}

	if has {
		return ErrDuplicatePinCollection
	}

	// Save the root pin reference.
	c.collection.Addr = root
	err = st.Put(c.collection)
	if err != nil {
		return fmt.Errorf("pin store: failed updating collection: %w", err)
	}

	err = st.Delete(&dirtyCollection{UUID: c.collection.UUID})
	if err != nil {
		return fmt.Errorf("pin store: failed deleting dirty collection: %w", err)
	}

	c.closed = true
	return nil
}

func (c *collectionPutter) Cleanup(st transaction.Storage) error {
	if c.closed {
		return nil
	}

	if err := deleteCollectionChunks(context.Background(), st, c.collection.UUID); err != nil {
		return fmt.Errorf("pin store: failed deleting collection chunks: %w", err)
	}

	err := st.Run(context.Background(), func(s transaction.Store) error {
		return s.IndexStore().Delete(&dirtyCollection{UUID: c.collection.UUID})
	})
	if err != nil {
		return fmt.Errorf("pin store: failed deleting dirty collection: %w", err)
	}

	c.closed = true
	return nil
}

// CleanupDirty will iterate over all the dirty collections and delete them.
func CleanupDirty(st transaction.Storage) error {

	dirtyCollections := make([]*dirtyCollection, 0)
	err := st.IndexStore().Iterate(
		storage.Query{
			Factory:      func() storage.Item { return new(dirtyCollection) },
			ItemProperty: storage.QueryItemID,
		},
		func(r storage.Result) (bool, error) {
			di := &dirtyCollection{UUID: []byte(r.ID)}
			dirtyCollections = append(dirtyCollections, di)
			return false, nil
		},
	)
	if err != nil {
		return fmt.Errorf("pin store: failed iterating dirty collections: %w", err)
	}

	for _, di := range dirtyCollections {
		err = errors.Join(err, (&collectionPutter{collection: &pinCollectionItem{UUID: di.UUID}}).Cleanup(st))
	}

	return err
}

// HasPin function will check if the address represents a valid pin collection.
func HasPin(st storage.Reader, root swarm.Address) (bool, error) {
	collection := &pinCollectionItem{Addr: root}
	has, err := st.Has(collection)
	if err != nil {
		return false, fmt.Errorf("pin store: failed checking collection: %w", err)
	}
	return has, nil
}

// Pins lists all the added pinning collections.
func Pins(st storage.Reader) ([]swarm.Address, error) {
	var pins []swarm.Address
	err := st.Iterate(storage.Query{
		Factory:      func() storage.Item { return new(pinCollectionItem) },
		ItemProperty: storage.QueryItemID,
	}, func(r storage.Result) (bool, error) {
		addr := swarm.NewAddress([]byte(r.ID))
		pins = append(pins, addr)
		return false, nil
	})
	if err != nil {
		return nil, fmt.Errorf("pin store: failed iterating root refs: %w", err)
	}

	return pins, nil
}

func deleteCollectionChunks(ctx context.Context, st transaction.Storage, collectionUUID []byte) error {
	chunksToDelete := make([]*pinChunkItem, 0)

	err := st.IndexStore().Iterate(
		storage.Query{
			Factory: func() storage.Item { return &pinChunkItem{UUID: collectionUUID} },
		}, func(r storage.Result) (bool, error) {
			addr := swarm.NewAddress([]byte(r.ID))
			chunk := &pinChunkItem{UUID: collectionUUID, Addr: addr}
			chunksToDelete = append(chunksToDelete, chunk)
			return false, nil
		},
	)
	if err != nil {
		return fmt.Errorf("pin store: failed iterating collection chunks: %w", err)
	}

	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(runtime.NumCPU())

	for _, item := range chunksToDelete {
		func(item *pinChunkItem) {
			eg.Go(func() error {
				return st.Run(ctx, func(s transaction.Store) error {
					return errors.Join(
						s.IndexStore().Delete(item),
						s.ChunkStore().Delete(ctx, item.Addr),
					)
				})
			})

		}(item)
	}

	err = eg.Wait()
	if err != nil {
		return fmt.Errorf("pin store: failed tx deleting collection chunks: %w", err)
	}

	return nil
}

// DeletePin will delete the root pin and all the chunks that are part of this collection.
func DeletePin(ctx context.Context, st transaction.Storage, root swarm.Address) error {
	collection := &pinCollectionItem{Addr: root}

	err := st.IndexStore().Get(collection)
	if err != nil {
		return fmt.Errorf("pin store: failed getting collection: %w", err)
	}

	if err := deleteCollectionChunks(ctx, st, collection.UUID); err != nil {
		return err
	}

	return st.Run(ctx, func(s transaction.Store) error {
		err := s.IndexStore().Delete(collection)
		if err != nil {
			return fmt.Errorf("pin store: failed deleting root collection: %w", err)
		}
		return nil
	})
}

func IterateCollection(st storage.Reader, root swarm.Address, fn func(addr swarm.Address) (bool, error)) error {
	collection := &pinCollectionItem{Addr: root}
	err := st.Get(collection)
	if err != nil {
		return fmt.Errorf("pin store: failed getting collection: %w", err)
	}

	return st.Iterate(storage.Query{
		Factory:      func() storage.Item { return &pinChunkItem{UUID: collection.UUID} },
		ItemProperty: storage.QueryItemID,
	}, func(r storage.Result) (bool, error) {
		addr := swarm.NewAddress([]byte(r.ID))
		stop, err := fn(addr)
		if err != nil {
			return true, err
		}
		return stop, nil
	})
}

func IterateCollectionStats(st storage.Reader, iterateFn func(st CollectionStat) (bool, error)) error {
	return st.Iterate(
		storage.Query{
			Factory: func() storage.Item { return new(pinCollectionItem) },
		},
		func(r storage.Result) (bool, error) {
			return iterateFn(r.Entry.(*pinCollectionItem).Stat)
		},
	)
}

// pinCollectionSize represents the size of the pinCollectionItem
const pinCollectionItemSize = encryption.ReferenceSize + uuidSize + 8 + 8

var _ storage.Item = (*pinCollectionItem)(nil)

// pinCollectionItem is the index used to describe a pinning collection. The Addr
// is the root reference of the collection and UUID is a unique UUID for this collection.
// The Address could be an encrypted swarm hash. This hash has the key to decrypt the
// collection.
type pinCollectionItem struct {
	Addr swarm.Address
	UUID []byte
	Stat CollectionStat
}

func (p *pinCollectionItem) ID() string { return p.Addr.ByteString() }

func (pinCollectionItem) Namespace() string { return "pinCollectionItem" }

func (p *pinCollectionItem) Marshal() ([]byte, error) {
	if p.Addr.IsZero() {
		return nil, errInvalidPinCollectionAddr
	}
	if len(p.UUID) == 0 {
		return nil, errInvalidPinCollectionUUID
	}
	buf := make([]byte, pinCollectionItemSize)
	copy(buf[:encryption.ReferenceSize], p.Addr.Bytes())
	off := encryption.ReferenceSize
	copy(buf[off:off+uuidSize], p.UUID)
	statBufOff := encryption.ReferenceSize + uuidSize
	binary.LittleEndian.PutUint64(buf[statBufOff:], p.Stat.Total)
	binary.LittleEndian.PutUint64(buf[statBufOff+8:], p.Stat.DupInCollection)
	return buf, nil
}

func (p *pinCollectionItem) Unmarshal(buf []byte) error {
	if len(buf) != pinCollectionItemSize {
		return errInvalidPinCollectionSize
	}
	ni := new(pinCollectionItem)
	if bytes.Equal(buf[swarm.HashSize:encryption.ReferenceSize], emptyKey) {
		ni.Addr = swarm.NewAddress(buf[:swarm.HashSize]).Clone()
	} else {
		ni.Addr = swarm.NewAddress(buf[:encryption.ReferenceSize]).Clone()
	}
	off := encryption.ReferenceSize
	ni.UUID = append(make([]byte, 0, uuidSize), buf[off:off+uuidSize]...)
	statBuf := buf[off+uuidSize:]
	ni.Stat.Total = binary.LittleEndian.Uint64(statBuf[:8])
	ni.Stat.DupInCollection = binary.LittleEndian.Uint64(statBuf[8:16])
	*p = *ni
	return nil
}

func (p *pinCollectionItem) Clone() storage.Item {
	if p == nil {
		return nil
	}
	return &pinCollectionItem{
		Addr: p.Addr.Clone(),
		UUID: append([]byte(nil), p.UUID...),
		Stat: p.Stat,
	}
}

func (p pinCollectionItem) String() string {
	return storageutil.JoinFields(p.Namespace(), p.ID())
}

var _ storage.Item = (*pinChunkItem)(nil)

// pinChunkItem is the index used to represent a single chunk in the pinning
// collection. It is prefixed with the UUID of the collection.
type pinChunkItem struct {
	UUID []byte
	Addr swarm.Address
}

func (p *pinChunkItem) Namespace() string { return string(p.UUID) }

func (p *pinChunkItem) ID() string { return p.Addr.ByteString() }

// pinChunkItem is a key-only type index. We don't need to store any value. As such
// the serialization functions would be no-ops. A Get operation on this key is not
// required as the key would constitute the item. Usually these type of indexes are
// useful for key-only iterations.
func (p *pinChunkItem) Marshal() ([]byte, error) {
	return nil, nil
}

func (p *pinChunkItem) Unmarshal(_ []byte) error {
	return nil
}

func (p *pinChunkItem) Clone() storage.Item {
	if p == nil {
		return nil
	}
	return &pinChunkItem{
		UUID: append([]byte(nil), p.UUID...),
		Addr: p.Addr.Clone(),
	}
}

func (p pinChunkItem) String() string {
	return storageutil.JoinFields(p.Namespace(), p.ID())
}

type dirtyCollection struct {
	UUID []byte
}

func (d *dirtyCollection) ID() string { return string(d.UUID) }

func (dirtyCollection) Namespace() string { return "dirtyCollection" }

func (d *dirtyCollection) Marshal() ([]byte, error) {
	return nil, nil
}

func (d *dirtyCollection) Unmarshal(_ []byte) error {
	return nil
}

func (d *dirtyCollection) Clone() storage.Item {
	if d == nil {
		return nil
	}
	return &dirtyCollection{
		UUID: append([]byte(nil), d.UUID...),
	}
}

func (d dirtyCollection) String() string {
	return storageutil.JoinFields(d.Namespace(), d.ID())
}
