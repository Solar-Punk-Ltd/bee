// Copyright 2020 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package retrieval_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethersphere/bee/v2/pkg/accounting"
	accountingmock "github.com/ethersphere/bee/v2/pkg/accounting/mock"
	"github.com/ethersphere/bee/v2/pkg/log"
	"github.com/ethersphere/bee/v2/pkg/p2p"
	"github.com/ethersphere/bee/v2/pkg/p2p/protobuf"
	"github.com/ethersphere/bee/v2/pkg/p2p/streamtest"
	"github.com/ethersphere/bee/v2/pkg/pricer"
	pricermock "github.com/ethersphere/bee/v2/pkg/pricer/mock"
	"github.com/ethersphere/bee/v2/pkg/retrieval"
	pb "github.com/ethersphere/bee/v2/pkg/retrieval/pb"
	"github.com/ethersphere/bee/v2/pkg/spinlock"
	"github.com/ethersphere/bee/v2/pkg/storage"
	"github.com/ethersphere/bee/v2/pkg/storage/inmemchunkstore"
	testingc "github.com/ethersphere/bee/v2/pkg/storage/testing"
	storemock "github.com/ethersphere/bee/v2/pkg/storer/mock"
	"github.com/ethersphere/bee/v2/pkg/swarm"
	"github.com/ethersphere/bee/v2/pkg/topology"
	"github.com/ethersphere/bee/v2/pkg/tracing"

	topologymock "github.com/ethersphere/bee/v2/pkg/topology/mock"
)

var (
	testTimeout  = 5 * time.Second
	defaultPrice = uint64(10)
)

type testStorer struct {
	storage.ChunkStore
}

func (t *testStorer) Lookup() storage.Getter { return t.ChunkStore }

func (t *testStorer) Cache() storage.Putter { return t.ChunkStore }

// TestDelivery tests that a naive request -> delivery flow works.
func TestDelivery(t *testing.T) {
	t.Parallel()

	var (
		chunk                = testingc.FixtureChunk("0033")
		logger               = log.Noop
		mockStorer           = &testStorer{ChunkStore: inmemchunkstore.New()}
		clientMockAccounting = accountingmock.NewAccounting()
		serverMockAccounting = accountingmock.NewAccounting()
		clientAddr           = swarm.MustParseHexAddress("9ee7add8")
		serverAddr           = swarm.MustParseHexAddress("9ee7add7")

		pricerMock = pricermock.NewMockService(defaultPrice, defaultPrice)
	)
	// put testdata in the mock store of the server
	err := mockStorer.Put(context.Background(), chunk)
	if err != nil {
		t.Fatal(err)
	}

	// create the server that will handle the request and will serve the response
	server := createRetrieval(t, swarm.MustParseHexAddress("0034"), mockStorer, nil, nil, logger, serverMockAccounting, pricerMock, nil, false)
	recorder := streamtest.New(
		streamtest.WithProtocols(server.Protocol()),
		streamtest.WithBaseAddr(clientAddr),
	)

	// client mock storer does not store any data at this point
	// but should be checked at the end of the test for the
	// presence of the chunk address key and value to ensure delivery
	// was successful
	clientMockStorer := &testStorer{ChunkStore: inmemchunkstore.New()}

	mt := topologymock.NewTopologyDriver(topologymock.WithClosestPeer(serverAddr))

	client := createRetrieval(t, clientAddr, clientMockStorer, recorder, mt, logger, clientMockAccounting, pricerMock, nil, false)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	v, err := client.RetrieveChunk(ctx, chunk.Address(), swarm.ZeroAddress)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(v.Data(), chunk.Data()) {
		t.Fatalf("request and response data not equal. got %s want %s", v, chunk.Data())
	}
	records, err := recorder.Records(serverAddr, "retrieval", "1.4.0", "retrieval")
	if err != nil {
		t.Fatal(err)
	}
	if l := len(records); l != 1 {
		t.Fatalf("got %v records, want %v", l, 1)
	}

	record := records[0]

	messages, err := protobuf.ReadMessages(
		bytes.NewReader(record.In()),
		func() protobuf.Message { return new(pb.Request) },
	)
	if err != nil {
		t.Fatal(err)
	}
	reqs := make([]string, 0, len(messages))
	for _, m := range messages {
		reqs = append(reqs, hex.EncodeToString(m.(*pb.Request).Addr))
	}

	if len(reqs) != 1 {
		t.Fatalf("got too many requests. want 1 got %d", len(reqs))
	}

	messages, err = protobuf.ReadMessages(
		bytes.NewReader(record.Out()),
		func() protobuf.Message { return new(pb.Delivery) },
	)
	if err != nil {
		t.Fatal(err)
	}

	gotDeliveries := make([]string, 0, len(messages))
	for _, m := range messages {
		gotDeliveries = append(gotDeliveries, string(m.(*pb.Delivery).Data))
	}

	if len(gotDeliveries) != 1 {
		t.Fatalf("got too many deliveries. want 1 got %d", len(gotDeliveries))
	}

	clientBalance, _ := clientMockAccounting.Balance(serverAddr)
	if clientBalance.Int64() != -int64(defaultPrice) {
		t.Fatalf("unexpected balance on client. want %d got %d", -defaultPrice, clientBalance)
	}

	serverBalance, _ := serverMockAccounting.Balance(clientAddr)
	if serverBalance.Int64() != int64(defaultPrice) {
		t.Fatalf("unexpected balance on server. want %d got %d", defaultPrice, serverBalance)
	}
}

func TestWaitForInflight(t *testing.T) {
	t.Parallel()

	var (
		chunk      = testingc.FixtureChunk("7000")
		logger     = log.Noop
		pricerMock = pricermock.NewMockService(defaultPrice, defaultPrice)

		badMockStorer           = &testStorer{ChunkStore: inmemchunkstore.New()}
		badServerMockAccounting = accountingmock.NewAccounting()
		badServerAddr           = swarm.MustParseHexAddress("6000000000000000000000000000000000000000000000000000000000000000")

		mockStorer           = &testStorer{ChunkStore: inmemchunkstore.New()}
		serverMockAccounting = accountingmock.NewAccounting()
		serverAddr           = swarm.MustParseHexAddress("5000000000000000000000000000000000000000000000000000000000000000")

		clientMockStorer     = &testStorer{ChunkStore: inmemchunkstore.New()}
		clientMockAccounting = accountingmock.NewAccounting()
		clientAddr           = swarm.MustParseHexAddress("9ee7add8")
	)

	// put testdata in the mock store of the server
	err := mockStorer.Put(context.Background(), chunk)
	if err != nil {
		t.Fatal(err)
	}

	// create the server that will handle the request and will serve the response
	server := createRetrieval(t, serverAddr, mockStorer, nil, nil, logger, serverMockAccounting, pricerMock, nil, false)

	badServer := createRetrieval(t, badServerAddr, badMockStorer, nil, nil, logger, badServerMockAccounting, pricerMock, nil, false)

	fail := true
	var lock sync.Mutex

	recorder := streamtest.New(
		streamtest.WithBaseAddr(clientAddr),
		streamtest.WithProtocols(badServer.Protocol(), server.Protocol()),
		streamtest.WithMiddlewares(func(h p2p.HandlerFunc) p2p.HandlerFunc {
			return func(ctx context.Context, p p2p.Peer, s p2p.Stream) error {
				lock.Lock()
				defer lock.Unlock()

				if fail {
					fail = false
					s.Close()
					return errors.New("peer not reachable")
				}

				time.Sleep(time.Second * 2)

				if err := h(ctx, p, s); err != nil {
					return err
				}
				// close stream after all previous middlewares wrote to it
				// so that the receiving peer can get all the post messages
				return s.Close()
			}
		}),
	)

	mt := topologymock.NewTopologyDriver(topologymock.WithPeers(badServerAddr, serverAddr))

	client := createRetrieval(t, clientAddr, clientMockStorer, recorder, mt, logger, clientMockAccounting, pricerMock, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*30)
	defer cancel()

	v, err := client.RetrieveChunk(ctx, chunk.Address(), swarm.ZeroAddress)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(v.Data(), chunk.Data()) {
		t.Fatalf("request and response data not equal. got %s want %s", v, chunk.Data())
	}
}

func TestRetrieveChunk(t *testing.T) {
	t.Parallel()

	var (
		logger = log.Noop
		pricer = pricermock.NewMockService(defaultPrice, defaultPrice)
	)

	// requesting a chunk from downstream peer is expected
	t.Run("downstream", func(t *testing.T) {
		t.Parallel()
		t.Skip()

		serverAddress := swarm.MustParseHexAddress("03")
		clientAddress := swarm.MustParseHexAddress("01")
		chunk := testingc.FixtureChunk("02c2")

		serverStorer := &testStorer{ChunkStore: inmemchunkstore.New()}
		err := serverStorer.Put(context.Background(), chunk)
		if err != nil {
			t.Fatal(err)
		}

		server := createRetrieval(t, serverAddress, serverStorer, nil, nil, logger, accountingmock.NewAccounting(), pricer, nil, false)
		recorder := streamtest.New(streamtest.WithProtocols(server.Protocol()))

		mt := topologymock.NewTopologyDriver(topologymock.WithClosestPeer(serverAddress))

		client := createRetrieval(t, clientAddress, nil, recorder, mt, logger, accountingmock.NewAccounting(), pricer, nil, false)

		got, err := client.RetrieveChunk(context.Background(), chunk.Address(), swarm.ZeroAddress)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Data(), chunk.Data()) {
			t.Fatalf("got data %x, want %x", got.Data(), chunk.Data())
		}
	})

	t.Run("forward", func(t *testing.T) {
		t.Parallel()
		t.Skip()

		chunk := testingc.FixtureChunk("0025")

		serverAddress := swarm.MustParseHexAddress("0100000000000000000000000000000000000000000000000000000000000000")
		forwarderAddress := swarm.MustParseHexAddress("0200000000000000000000000000000000000000000000000000000000000000")
		clientAddress := swarm.MustParseHexAddress("030000000000000000000000000000000000000000000000000000000000000000")

		serverStorer := &testStorer{ChunkStore: inmemchunkstore.New()}
		err := serverStorer.Put(context.Background(), chunk)
		if err != nil {
			t.Fatal(err)
		}

		server := createRetrieval(t,
			serverAddress,
			serverStorer, // chunk is in server's store
			nil,
			nil,
			logger,
			accountingmock.NewAccounting(),
			pricer,
			nil,
			false,
		)

		forwarderStore := &testStorer{ChunkStore: inmemchunkstore.New()}

		forwarder := createRetrieval(t,
			forwarderAddress,
			forwarderStore, // no chunk in forwarder's store
			streamtest.New(streamtest.WithProtocols(server.Protocol())), // connect to server
			topologymock.NewTopologyDriver(topologymock.WithClosestPeer(serverAddress)),
			logger,
			accountingmock.NewAccounting(),
			pricer,
			nil,
			true, // note explicit caching
		)

		client := createRetrieval(t,
			clientAddress,
			storemock.New(), // no chunk in clients's store
			streamtest.New(streamtest.WithProtocols(forwarder.Protocol())), // connect to forwarder
			topologymock.NewTopologyDriver(topologymock.WithClosestPeer(forwarderAddress)),
			logger,
			accountingmock.NewAccounting(),
			pricer,
			nil,
			false,
		)

		if got, _ := forwarderStore.Has(context.Background(), chunk.Address()); got {
			t.Fatalf("forwarder node already has chunk")
		}

		got, err := client.RetrieveChunk(context.Background(), chunk.Address(), swarm.ZeroAddress)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Data(), chunk.Data()) {
			t.Fatalf("got data %x, want %x", got.Data(), chunk.Data())
		}

		err = spinlock.Wait(time.Second, func() bool {
			gots, _ := forwarderStore.Has(context.Background(), chunk.Address())
			return gots
		})
		if err != nil {
			t.Fatalf("forwarder did not cache chunk")
		}
	})

	t.Run("propagate error to origin", func(t *testing.T) {
		t.Parallel()

		chunk := testingc.FixtureChunk("0025")

		serverAddress := swarm.MustParseHexAddress("0100000000000000000000000000000000000000000000000000000000000000")
		forwarderAddress := swarm.MustParseHexAddress("0200000000000000000000000000000000000000000000000000000000000000")
		clientAddress := swarm.MustParseHexAddress("030000000000000000000000000000000000000000000000000000000000000000")

		buf := new(bytes.Buffer)
		captureLogger := log.NewLogger("test", log.WithSink(buf))

		server := createRetrieval(t,
			serverAddress,
			&testStorer{ChunkStore: inmemchunkstore.New()},
			nil,
			topologymock.NewTopologyDriver(),
			logger,
			accountingmock.NewAccounting(),
			pricer,
			nil,
			false,
		)

		forwarderStore := &testStorer{ChunkStore: inmemchunkstore.New()}

		forwarder := createRetrieval(t,
			forwarderAddress,
			forwarderStore, // no chunk in forwarder's store
			streamtest.New(streamtest.WithProtocols(server.Protocol())), // connect to server
			topologymock.NewTopologyDriver(topologymock.WithClosestPeer(serverAddress)),
			logger,
			accountingmock.NewAccounting(),
			pricer,
			nil,
			true, // note explicit caching
		)

		client := createRetrieval(t,
			clientAddress,
			storemock.New(), // no chunk in clients's store
			streamtest.New(streamtest.WithProtocols(forwarder.Protocol())), // connect to forwarder
			topologymock.NewTopologyDriver(topologymock.WithClosestPeer(forwarderAddress)),
			captureLogger,
			accountingmock.NewAccounting(),
			pricer,
			nil,
			false,
		)

		_, err := client.RetrieveChunk(context.Background(), chunk.Address(), swarm.ZeroAddress)
		if err == nil {
			t.Fatal("should have received an error")
		}

		want := p2p.NewChunkDeliveryError("retrieve chunk: no peer found")
		if got := buf.String(); !strings.Contains(got, want.Error()) {
			t.Fatalf("got log %s, want %s", got, want)
		}
	})
}

func TestRetrievePreemptiveRetry(t *testing.T) {
	t.Parallel()

	logger := log.Noop

	chunk := testingc.FixtureChunk("0025")
	someOtherChunk := testingc.FixtureChunk("0033")

	pricerMock := pricermock.NewMockService(defaultPrice, defaultPrice)

	clientAddress := swarm.MustParseHexAddress("1010")

	serverAddress1 := swarm.MustParseHexAddress("1000000000000000000000000000000000000000000000000000000000000000")
	serverAddress2 := swarm.MustParseHexAddress("0200000000000000000000000000000000000000000000000000000000000000")
	peers := []swarm.Address{
		serverAddress1,
		serverAddress2,
	}

	serverStorer1 := &testStorer{ChunkStore: inmemchunkstore.New()}
	serverStorer2 := &testStorer{ChunkStore: inmemchunkstore.New()}

	// we put some other chunk on server 1
	err := serverStorer1.Put(context.Background(), someOtherChunk)
	if err != nil {
		t.Fatal(err)
	}
	// we put chunk we need on server 2
	err = serverStorer2.Put(context.Background(), chunk)
	if err != nil {
		t.Fatal(err)
	}

	noClosestPeer := topologymock.NewTopologyDriver()
	closetPeers := topologymock.NewTopologyDriver(topologymock.WithPeers(peers...))

	server1 := createRetrieval(t, serverAddress1, serverStorer1, nil, noClosestPeer, logger, accountingmock.NewAccounting(), pricerMock, nil, false)
	server2 := createRetrieval(t, serverAddress2, serverStorer2, nil, noClosestPeer, logger, accountingmock.NewAccounting(), pricerMock, nil, false)

	t.Run("peer not reachable", func(t *testing.T) {
		t.Parallel()

		ranOnce := true
		ranMux := sync.Mutex{}
		recorder := streamtest.New(
			streamtest.WithProtocols(
				server1.Protocol(),
				server2.Protocol(),
			),
			streamtest.WithMiddlewares(
				func(h p2p.HandlerFunc) p2p.HandlerFunc {
					return func(ctx context.Context, peer p2p.Peer, stream p2p.Stream) error {
						ranMux.Lock()
						defer ranMux.Unlock()
						// NOTE: return error for peer1
						if ranOnce {
							ranOnce = false
							return fmt.Errorf("peer not reachable: %s", peer.Address.String())
						}

						return server2.Handler(ctx, peer, stream)
					}
				},
			),
			streamtest.WithBaseAddr(clientAddress),
		)

		client := createRetrieval(t, clientAddress, nil, recorder, closetPeers, logger, accountingmock.NewAccounting(), pricerMock, nil, false)

		got, err := client.RetrieveChunk(context.Background(), chunk.Address(), swarm.ZeroAddress)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got.Data(), chunk.Data()) {
			t.Fatalf("got data %x, want %x", got.Data(), chunk.Data())
		}
	})

	t.Run("peer does not have chunk", func(t *testing.T) {
		t.Parallel()

		ranOnce := true
		ranMux := sync.Mutex{}
		recorder := streamtest.New(
			streamtest.WithProtocols(
				server1.Protocol(),
				server2.Protocol(),
			),
			streamtest.WithMiddlewares(
				func(h p2p.HandlerFunc) p2p.HandlerFunc {
					return func(ctx context.Context, peer p2p.Peer, stream p2p.Stream) error {
						ranMux.Lock()
						defer ranMux.Unlock()
						if ranOnce {
							ranOnce = false
							return server1.Handler(ctx, peer, stream)
						}

						return server2.Handler(ctx, peer, stream)
					}
				},
			),
		)

		client := createRetrieval(t, clientAddress, nil, recorder, closetPeers, logger, accountingmock.NewAccounting(), pricerMock, nil, false)

		got, err := client.RetrieveChunk(context.Background(), chunk.Address(), swarm.ZeroAddress)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got.Data(), chunk.Data()) {
			t.Fatalf("got data %x, want %x", got.Data(), chunk.Data())
		}
	})

	t.Run("one peer is slower", func(t *testing.T) {
		t.Parallel()

		serverStorer1 := &testStorer{ChunkStore: inmemchunkstore.New()}
		serverStorer2 := &testStorer{ChunkStore: inmemchunkstore.New()}

		// both peers have required chunk
		err := serverStorer1.Put(context.Background(), chunk)
		if err != nil {
			t.Fatal(err)
		}
		err = serverStorer2.Put(context.Background(), chunk)
		if err != nil {
			t.Fatal(err)
		}

		server1MockAccounting := accountingmock.NewAccounting()
		server2MockAccounting := accountingmock.NewAccounting()

		server1 := createRetrieval(t, serverAddress1, serverStorer1, nil, noClosestPeer, logger, server1MockAccounting, pricerMock, nil, false)
		server2 := createRetrieval(t, serverAddress2, serverStorer2, nil, noClosestPeer, logger, server2MockAccounting, pricerMock, nil, false)

		// NOTE: must be more than retry duration
		// (here one second more)
		server1ResponseDelayDuration := 2 * time.Second

		ranOnce := true
		ranMux := sync.Mutex{}
		recorder := streamtest.New(
			streamtest.WithProtocols(
				server1.Protocol(),
				server2.Protocol(),
			),
			streamtest.WithMiddlewares(
				func(h p2p.HandlerFunc) p2p.HandlerFunc {
					return func(ctx context.Context, peer p2p.Peer, stream p2p.Stream) error {
						ranMux.Lock()
						if ranOnce {
							// NOTE: sleep time must be more than retry duration
							ranOnce = false
							ranMux.Unlock()
							time.Sleep(server1ResponseDelayDuration)
							// server2 is picked first because it's address is closer to the chunk than server1
							return server2.Handler(ctx, peer, stream)
						}
						ranMux.Unlock()

						return server1.Handler(ctx, peer, stream)
					}
				},
			),
		)

		clientMockAccounting := accountingmock.NewAccounting()

		client := createRetrieval(t, clientAddress, nil, recorder, closetPeers, logger, clientMockAccounting, pricerMock, nil, false)

		got, err := client.RetrieveChunk(context.Background(), chunk.Address(), swarm.ZeroAddress)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got.Data(), chunk.Data()) {
			t.Fatalf("got data %x, want %x", got.Data(), chunk.Data())
		}

		clientServer1Balance, _ := clientMockAccounting.Balance(serverAddress1)
		if clientServer1Balance.Int64() != -int64(defaultPrice) {
			t.Fatalf("unexpected balance on client. want %d got %d", -int64(defaultPrice), clientServer1Balance)
		}

		clientServer2Balance, _ := clientMockAccounting.Balance(serverAddress2)
		if clientServer2Balance.Int64() != 0 {
			t.Fatalf("unexpected balance on client. want %d got %d", 0, clientServer2Balance)
		}

		// wait and check balance again
		// (yet one second more than before, minus original duration)
		time.Sleep(2 * time.Second)

		clientServer1Balance, _ = clientMockAccounting.Balance(serverAddress1)
		if clientServer1Balance.Int64() != -int64(defaultPrice) {
			t.Fatalf("unexpected balance on client. want %d got %d", -int64(defaultPrice), clientServer1Balance)
		}

		clientServer2Balance, _ = clientMockAccounting.Balance(serverAddress2)
		if clientServer2Balance.Int64() != -int64(defaultPrice) {
			t.Fatalf("unexpected balance on client. want %d got %d", -int64(defaultPrice), clientServer2Balance)
		}
	})

	t.Run("peer forwards request", func(t *testing.T) {
		t.Parallel()

		// server 2 has the chunk
		server2 := createRetrieval(t, serverAddress2, serverStorer2, nil, noClosestPeer, logger, accountingmock.NewAccounting(), pricerMock, nil, false)

		server1Recorder := streamtest.New(
			streamtest.WithProtocols(server2.Protocol()),
		)

		// server 1 will forward request to server 2
		server1 := createRetrieval(t, serverAddress1, serverStorer1, server1Recorder, topologymock.NewTopologyDriver(topologymock.WithPeers(serverAddress2)), logger, accountingmock.NewAccounting(), pricerMock, nil, true)

		clientRecorder := streamtest.New(
			streamtest.WithProtocols(server1.Protocol()),
		)

		// client only knows about server 1
		client := createRetrieval(t, clientAddress, nil, clientRecorder, topologymock.NewTopologyDriver(topologymock.WithPeers(serverAddress1)), logger, accountingmock.NewAccounting(), pricerMock, nil, false)

		if got, _ := serverStorer1.Has(context.Background(), chunk.Address()); got {
			t.Fatalf("forwarder node already has chunk")
		}

		got, err := client.RetrieveChunk(context.Background(), chunk.Address(), swarm.ZeroAddress)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got.Data(), chunk.Data()) {
			t.Fatalf("got data %x, want %x", got.Data(), chunk.Data())
		}
		err = spinlock.Wait(time.Second, func() bool {
			has, _ := serverStorer1.Has(context.Background(), chunk.Address())
			return has
		})
		if err != nil {
			t.Fatalf("forwarder node does not have chunk")
		}
	})
}

func TestClosestPeer(t *testing.T) {
	t.Parallel()

	srvAd := swarm.MustParseHexAddress("0100000000000000000000000000000000000000000000000000000000000000")

	addr1 := swarm.MustParseHexAddress("0200000000000000000000000000000000000000000000000000000000000000")
	addr2 := swarm.MustParseHexAddress("0300000000000000000000000000000000000000000000000000000000000000")
	addr3 := swarm.MustParseHexAddress("0400000000000000000000000000000000000000000000000000000000000000")

	ret := createRetrieval(t, srvAd, nil, nil, topologymock.NewTopologyDriver(topologymock.WithPeers(addr1, addr2, addr3)), log.Noop, nil, nil, nil, false)

	t.Run("closest", func(t *testing.T) {
		t.Parallel()

		addr, err := ret.ClosestPeer(addr1, nil, false)
		if err != nil {
			t.Fatal("closest peer", err)
		}
		if !addr.Equal(addr1) {
			t.Fatalf("want %s, got %s", addr1.String(), addr.String())
		}
	})

	t.Run("second closest", func(t *testing.T) {
		t.Parallel()

		addr, err := ret.ClosestPeer(addr1, []swarm.Address{addr1}, false)
		if err != nil {
			t.Fatal("closest peer", err)
		}
		if !addr.Equal(addr2) {
			t.Fatalf("want %s, got %s", addr2.String(), addr.String())
		}
	})

	t.Run("closest is further than base addr", func(t *testing.T) {
		t.Parallel()

		_, err := ret.ClosestPeer(srvAd, nil, false)
		if !errors.Is(err, topology.ErrNotFound) {
			t.Fatal("closest peer", err)
		}
	})
}

// TestOverDraftRefreshCounted covers the one wait on the retrieval path that bee performs and does not
// report. When every candidate peer is overdrafted, retrieval sleeps overDraftRefresh for the peers'
// allowance to accrue and then retries, so a node throttled by settlement is indistinguishable from a
// slow network in every series bee publishes.
//
// Requests are made non-origin deliberately. An origin request arms a one-second preemptive ticker
// whose retries race the sleeps, which would make the counts here a range rather than a number.
func TestOverDraftRefreshCounted(t *testing.T) {
	t.Parallel()

	// Full-length addresses, because the peer-list branch of the topology mock compares distances and
	// refuses to compare a short address with a long one. The client sits at ff.. so that every chunk
	// below is nearer the server than the client: a non-origin request refuses an upstream peer, and a
	// client nearer the chunk than its only peer retrieves nothing at all.
	const (
		serverHex = "0300000000000000000000000000000000000000000000000000000000000000"
		clientHex = "ff00000000000000000000000000000000000000000000000000000000000000"
		sourceHex = "0900000000000000000000000000000000000000000000000000000000000000"
	)

	// newClient returns a retrieval service whose only peer refuses the first overdrafts credit
	// preparations and accepts every one after that.
	newClient := func(t *testing.T, chunk swarm.Chunk, overdrafts int32) *retrieval.Service {
		t.Helper()

		pricer := pricermock.NewMockService(defaultPrice, defaultPrice)
		serverAddress := swarm.MustParseHexAddress(serverHex)

		serverStorer := &testStorer{ChunkStore: inmemchunkstore.New()}
		if err := serverStorer.Put(context.Background(), chunk); err != nil {
			t.Fatal(err)
		}
		server := createRetrieval(t, serverAddress, serverStorer, nil, nil, log.Noop,
			accountingmock.NewAccounting(), pricer, nil, false)

		var (
			prepared atomic.Int32
			credit   *accountingmock.Service
		)
		credit = accountingmock.NewAccounting(accountingmock.WithPrepareCreditFunc(
			func(peer swarm.Address, price uint64, _ bool) (accounting.Action, error) {
				if prepared.Add(1) <= overdrafts {
					return nil, accounting.ErrOverdraft
				}
				return credit.MakeCreditAction(peer, price), nil
			}))

		// WithPeers, not WithClosestPeer: only the peer-list branch of the topology mock honours
		// skipPeers, and a peer that cannot be skipped never drives closestPeer to ErrNotFound, which
		// is the branch that sleeps.
		peers := topologymock.NewTopologyDriver(topologymock.WithPeers(serverAddress))

		return createRetrieval(t, swarm.MustParseHexAddress(clientHex), nil,
			streamtest.New(streamtest.WithProtocols(server.Protocol())), peers, log.Noop, credit, pricer, nil, false)
	}

	retrieve := func(t *testing.T, client *retrieval.Service, chunk swarm.Chunk) {
		t.Helper()

		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		got, err := client.RetrieveChunk(ctx, chunk.Address(), swarm.MustParseHexAddress(sourceHex))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Data(), chunk.Data()) {
			t.Fatalf("got data %x, want %x", got.Data(), chunk.Data())
		}
	}

	for _, tc := range []struct {
		name       string
		fixture    string
		overdrafts int32
	}{
		{name: "a peer with allowance never sleeps", fixture: "0025", overdrafts: 0},
		{name: "one overdraft is one sleep", fixture: "02c2", overdrafts: 1},
		{name: "two overdrafts are two sleeps", fixture: "7000", overdrafts: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			chunk := testingc.FixtureChunk(tc.fixture)
			client := newClient(t, chunk, tc.overdrafts)

			if before := overDraftSleeps(t, client); before != 0 {
				t.Fatalf("counter started at %v, want 0", before)
			}

			started := time.Now()
			retrieve(t, client, chunk)
			elapsed := time.Since(started)

			if got := overDraftSleeps(t, client); got != float64(tc.overdrafts) {
				t.Fatalf("counted %v overdraft sleeps, want %d", got, tc.overdrafts)
			}

			// The count is only worth publishing if it means the wall time it claims to. Each sleep is
			// a full overDraftRefresh, so the request cannot have finished sooner than their sum.
			if want := retrieval.OverDraftRefresh * time.Duration(tc.overdrafts); elapsed < want {
				t.Fatalf("retrieval took %s, too fast to have slept %d times (%s)", elapsed, tc.overdrafts, want)
			}
		})
	}
}

// TestOverDraftRefreshNotCountedWhenCancelled proves the counter records sleeps that completed rather
// than sleeps that were entered. A request abandoned mid-wait never got its allowance back, and
// counting it would overstate the settlement cost of every cancelled read.
func TestOverDraftRefreshNotCountedWhenCancelled(t *testing.T) {
	t.Parallel()

	chunk := testingc.FixtureChunk("0025")
	pricer := pricermock.NewMockService(defaultPrice, defaultPrice)
	serverAddress := swarm.MustParseHexAddress("0300000000000000000000000000000000000000000000000000000000000000")

	serverStorer := &testStorer{ChunkStore: inmemchunkstore.New()}
	if err := serverStorer.Put(context.Background(), chunk); err != nil {
		t.Fatal(err)
	}
	server := createRetrieval(t, serverAddress, serverStorer, nil, nil, log.Noop,
		accountingmock.NewAccounting(), pricer, nil, false)

	// Always overdrafted, so the retrieval can only ever be sleeping or about to.
	credit := accountingmock.NewAccounting(accountingmock.WithPrepareCreditFunc(
		func(swarm.Address, uint64, bool) (accounting.Action, error) {
			return nil, accounting.ErrOverdraft
		}))

	client := createRetrieval(t, swarm.MustParseHexAddress("ff00000000000000000000000000000000000000000000000000000000000000"), nil,
		streamtest.New(streamtest.WithProtocols(server.Protocol())),
		topologymock.NewTopologyDriver(topologymock.WithPeers(serverAddress)),
		log.Noop, credit, pricer, nil, false)

	// Well inside the first sleep, so it is cancelled having waited and never having finished waiting.
	ctx, cancel := context.WithTimeout(context.Background(), retrieval.OverDraftRefresh/3)
	defer cancel()

	source := swarm.MustParseHexAddress("0900000000000000000000000000000000000000000000000000000000000000")
	if _, err := client.RetrieveChunk(ctx, chunk.Address(), source); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("retrieval returned %v, want %v", err, context.DeadlineExceeded)
	}
	if got := overDraftSleeps(t, client); got != 0 {
		t.Fatalf("counted %v sleeps, want 0: a cancelled wait is not a completed one", got)
	}
}

func overDraftSleeps(t *testing.T, s *retrieval.Service) float64 {
	t.Helper()

	count, err := s.OverDraftRefreshCount()
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func createRetrieval(
	t *testing.T,
	addr swarm.Address,
	storer retrieval.Storer,
	streamer p2p.Streamer,
	chunkPeerer topology.ClosestPeerer,
	logger log.Logger,
	accounting accounting.Interface,
	pricer pricer.Interface,
	tracer *tracing.Tracer,
	forwarderCaching bool,
) *retrieval.Service {
	t.Helper()

	radiusF := func() (uint8, error) { return swarm.MaxBins, nil }

	ret := retrieval.New(addr, radiusF, storer, streamer, chunkPeerer, logger, accounting, pricer, tracer, forwarderCaching)
	t.Cleanup(func() { ret.Close() })
	return ret
}

// TestRetrieveAttemptsSurviveCancellation covers a data race rather than a behaviour: singleflight
// returns to a caller whose context is done while the shared closure keeps running for the other
// waiters, so RetrieveChunk's deferred metrics observation reads the attempt counter that closure is
// still incrementing. Only `go test -race` can fail this, and without the fix it does, every time.
func TestRetrieveAttemptsSurviveCancellation(t *testing.T) {
	t.Parallel()

	chunk := testingc.FixtureChunk("0025")
	pricer := pricermock.NewMockService(defaultPrice, defaultPrice)
	serverAddress := swarm.MustParseHexAddress("0300000000000000000000000000000000000000000000000000000000000000")

	serverStorer := &testStorer{ChunkStore: inmemchunkstore.New()}
	if err := serverStorer.Put(context.Background(), chunk); err != nil {
		t.Fatal(err)
	}
	server := createRetrieval(t, serverAddress, serverStorer, nil, nil, log.Noop,
		accountingmock.NewAccounting(), pricer, nil, false)

	// A permanently overdrafted peer keeps the closure in its retry loop for the whole test, which is
	// what guarantees it is still running when the cancelled caller returns and reads.
	credit := accountingmock.NewAccounting(accountingmock.WithPrepareCreditFunc(
		func(swarm.Address, uint64, bool) (accounting.Action, error) {
			return nil, accounting.ErrOverdraft
		}))

	client := createRetrieval(t, swarm.MustParseHexAddress("ff00000000000000000000000000000000000000000000000000000000000000"), nil,
		streamtest.New(streamtest.WithProtocols(server.Protocol())),
		topologymock.NewTopologyDriver(topologymock.WithPeers(serverAddress)),
		log.Noop, credit, pricer, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	source := swarm.MustParseHexAddress("0900000000000000000000000000000000000000000000000000000000000000")
	if _, err := client.RetrieveChunk(ctx, chunk.Address(), source); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("retrieval returned %v, want %v", err, context.DeadlineExceeded)
	}
}
