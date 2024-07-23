// Copyright 2020 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"net/http"
	"os"
	"path"

	"github.com/ethersphere/bee/v2/pkg/plugin"
)

type pluginResponse struct {
	Res string `json:"res"`
}

func (s *Service) pluginLoadHandler(w http.ResponseWriter, r *http.Request) {
	logger := s.logger.WithName("plugin").Build()

	// ctx := r.Context()

	pluginPath := "bee-plugins"
	plugins, err := os.ReadDir(pluginPath)
	if err != nil {
		logger.Error(err, "unable to read plugin directory")
	}
	for _, p := range plugins {
		_, err := plugin.Register(path.Join(pluginPath, p.Name()))
		if err != nil {
			return
		}
	}
	return
}

func (s *Service) pluginUnLoadHandler(w http.ResponseWriter, r *http.Request) {
	// logger := s.logger.WithName("plugin").Build()

	// ctx := r.Context()

	//TODO
	return
}
