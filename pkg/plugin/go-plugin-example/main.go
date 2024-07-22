package main

import (
	"fmt"

	"github.com/ethersphere/bee/v2/pkg/plugin/shared"
	"github.com/hashicorp/go-plugin"
)

type ExamplePlugin struct{}

func (ExamplePlugin) Fn(value string) (string, error) {
	r := []rune(value)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return fmt.Sprintf("%s2", string(r)), nil
}

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.Handshake,
		Plugins: map[string]plugin.Plugin{
			"kv_grpc": &shared.BeeGRPCPlugin{Impl: &ExamplePlugin{}},
		},

		GRPCServer: plugin.DefaultGRPCServer,
	})
}
