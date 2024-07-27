package shared

import (
	"context"

	"google.golang.org/grpc"

	"github.com/ethersphere/bee/v2/pkg/plugin/pb"
	"github.com/hashicorp/go-plugin"
)

// Handshake is a common handshake configuration for all plugins.
// MagicCookieKey and MagicCookieValue shoud be unique, and must not change
// during the lifetime of the program.
//
//nolint:gochecknoglobals
var Handshake = plugin.HandshakeConfig{
	// This isn't required when using VersionedPlugins
	ProtocolVersion:  1,
	MagicCookieKey:   "BEE_PLUGIN",
	MagicCookieValue: "0190dacf-06ed-70cb-aef4-9811778d99fe",
}

// PluginMap is the map of plugins we can dispense.
// Since we only want to use gRPC there is only one key.
var PluginMap = map[string]plugin.Plugin{
	"bee_grpc_plugin": &BeeGRPCPlugin{},
}

// BeePlugin is the interface that we're exposing as a plugin.
type BeePlugin interface {
	Fn(value string) (string, error)
}

// BeeGRPCPlugin is the implementation of plugin.Plugin so we can serve/consume this.
type BeeGRPCPlugin struct {
	plugin.Plugin
	Impl BeePlugin
}

// GRPCServer is the implementation of gRPC server for the plugin.
func (p *BeeGRPCPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	pb.RegisterBeePluginServer(s, &GRPCServer{Impl: p.Impl})
	return nil
}

// GRPCClient is the implementation of gRPC client for the plugin.
func (p *BeeGRPCPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &GRPCClient{client: pb.NewBeePluginClient(c)}, nil
}
