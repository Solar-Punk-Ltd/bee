package plugin

import (
	"os/exec"

	"github.com/ethersphere/bee/v2/pkg/plugin/shared"
	"github.com/hashicorp/go-plugin"
)

type Plugin struct {
	Client    *plugin.Client
	BeePlugin shared.BeePlugin
}

func (p *Plugin) Close() {
	p.Client.Kill()
}

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

// func main() {
// 	plugins := []*Plugin{}
// 	p, err := register("./go-plugin-example/go-plugin-example")
// 	defer p.Client.Kill()
// 	if err != nil {
// 		panic(err)
// 	}
// 	plugins = append(plugins, p)

// 	p2, err := register("./go-plugin-example/go-plugin-example2")
// 	defer p2.Client.Kill()
// 	if err != nil {
// 		panic(err)
// 	}
// 	plugins = append(plugins, p2)

// 	req := os.Args[1]
// 	for _, pl := range plugins {
// 		res, err := pl.BeePlugin.Fn(req)
// 		if err != nil {
// 			panic(err)
// 		}
// 		fmt.Println(res)
// 	}
// }
