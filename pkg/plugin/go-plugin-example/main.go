package main

import (
	"fmt"

	"github.com/ethersphere/bee/v2/pkg/plugin/shared"
	"github.com/hashicorp/go-plugin"
)

// ExamplePlugin is an example implementation of the BeePlugin interface.
// It has only one funtion that reverses the input string.
type ExamplePlugin struct{}

// Fn is the one function that the plugins need to implement.
func (ExamplePlugin) Fn(value string) (string, error) {
	r := []rune(value)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return fmt.Sprintf("%s", string(r)), nil
}

// main is the entry point of the plugin.
// The plugin needs to be an executable that serves the gRPC server.
// The plugin won't run in itself, but will be loaded by the main application.
func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.Handshake,
		Plugins: map[string]plugin.Plugin{
			"kv_grpc": &shared.BeeGRPCPlugin{Impl: &ExamplePlugin{}},
		},

		GRPCServer: plugin.DefaultGRPCServer,
	})
}
