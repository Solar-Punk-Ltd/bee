package shared

import (
	"context"

	"github.com/ethersphere/bee/v2/pkg/plugin/pb"
)

// GRPCClient is the implementation of gRPC client for the plugin.
type GRPCClient struct{ client pb.BeePluginClient }

// Fn calls the remote Fn function.
func (c *GRPCClient) Fn(value string) (string, error) {
	resp, err := c.client.Fn(context.Background(), &pb.PluginRequest{
		Value: value,
	})
	if err != nil {
		return "", err
	}

	return resp.GetValue(), nil
}

// GRPCServer is the implementation of gRPC server for the plugin.
type GRPCServer struct {
	Impl BeePlugin
}

// Fn calls the local Fn function.
func (s *GRPCServer) Fn(_ context.Context, req *pb.PluginRequest) (*pb.PluginResponse, error) {
	v, err := s.Impl.Fn(req.GetValue())
	return &pb.PluginResponse{Value: v}, err
}
