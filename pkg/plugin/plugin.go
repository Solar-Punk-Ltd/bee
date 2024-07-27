package plugin

import (
	"os/exec"

	"github.com/ethersphere/bee/v2/pkg/plugin/shared"
	"github.com/hashicorp/go-plugin"
)

// Plugin is a wrapper for the plugin client and the plugin implementation.
type Plugin struct {
	Client    *plugin.Client
	BeePlugin shared.BeePlugin
}

// Close closes the plugin client.
func (p *Plugin) Close() {
	p.Client.Kill()
}

// Register is a wrappef function for loading a plugin.
func Register(cmd string) (*Plugin, error) {
	p := Plugin{}
	p.Client = plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: shared.Handshake,
		Plugins:         shared.PluginMap,
		Cmd:             exec.Command(cmd),
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolGRPC},
	})

	rpcClient, err := p.Client.Client()
	if err != nil {
		p.Client.Kill()
		return nil, err
	}

	raw, err := rpcClient.Dispense("bee_grpc_plugin")
	if err != nil {
		p.Client.Kill()
		return nil, err
	}

	p.BeePlugin = raw.(shared.BeePlugin)
	return &p, nil
}
