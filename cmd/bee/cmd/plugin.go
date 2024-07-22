package cmd

import (
	"os"
	"path"

	"github.com/ethersphere/bee/v2/pkg/plugin"
)

func (c *command) loadPlugins() error {
	pluginPath := path.Join(c.homeDir, "bee-plugins")
	plugins, err := os.ReadDir(pluginPath)
	if err != nil {
		return err
	}
	// fmt.Println(c.homeDir)
	for _, p := range plugins {
		_, err := plugin.Register(path.Join(pluginPath, p.Name()))
		if err != nil {
			return err
		}
	}
	return nil
}
