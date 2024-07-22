package shared

import (
	"context"

	"google.golang.org/grpc"

	"github.com/ethersphere/bee/v2/pkg/plugin/pb"
	"github.com/hashicorp/go-plugin"
)

var Handshake = plugin.HandshakeConfig{
	// This isn't required when using VersionedPlugins
	ProtocolVersion:  1,
	MagicCookieKey:   "BEE_PLUGIN",
	MagicCookieValue: "0190dacf-06ed-70cb-aef4-9811778d99fe",
}

var PluginMap = map[string]plugin.Plugin{
	"bee_grpc_plugin": &BeeGRPCPlugin{},
}

type BeePlugin interface {
	Fn(value string) (string, error)
}

type BeeGRPCPlugin struct {
	plugin.Plugin
	Impl BeePlugin
}

func (p *BeeGRPCPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	pb.RegisterBeePluginServer(s, &GRPCServer{Impl: p.Impl})
	return nil
}

func (p *BeeGRPCPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &GRPCClient{client: pb.NewBeePluginClient(c)}, nil
}
