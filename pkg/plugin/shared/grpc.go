package shared

import (
	"context"

	"github.com/ethersphere/bee/v2/pkg/plugin/pb"
)

type GRPCClient struct{ client pb.BeePluginClient }

func (c *GRPCClient) Fn(value string) (string, error) {
	resp, err := c.client.Fn(context.Background(), &pb.PluginRequest{
		Value: value,
	})
	if err != nil {
		return "", err
	}

	return resp.Value, nil
}

type GRPCServer struct {
	Impl BeePlugin
}

func (s *GRPCServer) Fn(
	ctx context.Context,
	req *pb.PluginRequest) (*pb.PluginResponse, error) {
	v, err := s.Impl.Fn(req.Value)
	return &pb.PluginResponse{Value: v}, err
}
