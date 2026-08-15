// Copyright 2020 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package retrieval

import (
	"context"

	dto "github.com/prometheus/client_model/go"

	"github.com/ethersphere/bee/v2/pkg/p2p"
	"github.com/ethersphere/bee/v2/pkg/swarm"
)

const OverDraftRefresh = overDraftRefresh

func (s *Service) OverDraftRefreshCount() (float64, error) {
	var m dto.Metric
	if err := s.metrics.OverDraftRefreshCount.Write(&m); err != nil {
		return 0, err
	}
	return m.GetCounter().GetValue(), nil
}

func (s *Service) Handler(ctx context.Context, p p2p.Peer, stream p2p.Stream) error {
	return s.handler(ctx, p, stream)
}

func (s *Service) ClosestPeer(addr swarm.Address, skipPeers []swarm.Address, allowUpstream bool) (swarm.Address, error) {
	return s.closestPeer(addr, skipPeers, allowUpstream)
}
