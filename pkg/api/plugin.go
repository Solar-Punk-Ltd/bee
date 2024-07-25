// Copyright 2020 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path"

	"github.com/ethersphere/bee/v2/pkg/jsonhttp"
	"github.com/ethersphere/bee/v2/pkg/plugin"
	"github.com/gorilla/mux"
)

type pluginResponse struct {
	Res string `json:"res"`
}

func (s *Service) pluginListHandler(w http.ResponseWriter, r *http.Request) {
	logger := s.logger.WithName("plugin").Build()

	plugins, err := os.ReadDir(s.pluginDir)
	if err != nil {
		logger.Error(err, "unable to read plugin directory")
		jsonhttp.InternalServerError(w, "unable to read plugin directory")
		return
	}

	pluginIDs := []string{}
	for _, p := range plugins {
		pluginIDs = append(pluginIDs, p.Name())
	}

	jsonhttp.OK(w, pluginIDs)
}

func (s *Service) pluginLoadHandler(w http.ResponseWriter, r *http.Request) {
	logger := s.logger.WithName("plugin").Build()

	paths := struct {
		ID string `map:"pluginID" validate:"required"`
	}{}
	if response := s.mapStructure(mux.Vars(r), &paths); response != nil {
		response("invalid path params", logger, w)
		return
	}

	plugin, err := plugin.Register(path.Join(s.pluginDir, paths.ID))
	s.Plugins[paths.ID] = plugin

	if err != nil {
		logger.Error(err, "unable to load plugin")
		jsonhttp.InternalServerError(w, "unable to load plugin")
		return
	}

	jsonhttp.OK(w, paths.ID)
}

func (s *Service) pluginUnLoadHandler(w http.ResponseWriter, r *http.Request) {
	logger := s.logger.WithName("plugin").Build()

	paths := struct {
		ID string `map:"pluginID" validate:"required"`
	}{}

	if response := s.mapStructure(mux.Vars(r), &paths); response != nil {
		response("invalid path params", logger, w)
		jsonhttp.BadRequest(w, "invalid path params")
		return
	}

	if _, ok := s.Plugins[paths.ID]; !ok {
		logger.Error(errors.New("plugin not found"), paths.ID)
		jsonhttp.NotFound(w, "plugin not found")
		return
	}

	s.Plugins[paths.ID].Close()
}

func (s *Service) pluginFnHandler(w http.ResponseWriter, r *http.Request) {
	logger := s.logger.WithName("plugin").Build()

	paths := struct {
		ID string `map:"pluginID" validate:"required"`
	}{}

	if response := s.mapStructure(mux.Vars(r), &paths); response != nil {
		response("invalid path params", logger, w)
		jsonhttp.BadRequest(w, "invalid path params")
		return
	}

	if _, ok := s.Plugins[paths.ID]; !ok {
		logger.Error(errors.New("plugin not found"), paths.ID)
		jsonhttp.NotFound(w, "plugin not found")
		return
	}

	req, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error(err, "failed to read request body")
		jsonhttp.InternalServerError(w, "failed to read request body")
		return
	}

	res, err := s.Plugins[paths.ID].BeePlugin.Fn(string(req))
	if err != nil {
		logger.Error(err, "plugin call failed")
		jsonhttp.InternalServerError(w, "plugin call failed")
		return
	}

	jsonhttp.OK(w, pluginResponse{res})
}
