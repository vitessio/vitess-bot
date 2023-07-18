/*
Copyright 2023 The Vitess Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gregjones/httpcache"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/rcrowley/go-metrics"
	"github.com/rs/zerolog"
)

func main() {
	cfg, err := readConfig()
	if err != nil {
		panic(err)
	}

	var f io.Writer
	if cfg.logFile != "" {
		f, err = os.OpenFile(cfg.logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, os.ModePerm)
		if err != nil {
			panic(err)
		}
	} else {
		f = os.Stdout
	}

	logger := zerolog.New(f).With().Timestamp().Logger()
	zerolog.DefaultContextLogger = &logger

	metricsRegistry := metrics.DefaultRegistry

	cc, err := githubapp.NewDefaultCachingClientCreator(
		cfg.Github,
		githubapp.WithClientUserAgent("vitess-bot/1.0.0"),
		githubapp.WithClientTimeout(30*time.Second),
		githubapp.WithClientCaching(false, func() httpcache.Cache { return httpcache.NewMemoryCache() }),
		githubapp.WithClientMiddleware(
			githubapp.ClientMetrics(metricsRegistry),
		),
	)
	if err != nil {
		panic(err)
	}

	prCommentHandler := &PullRequestHandler{
		ClientCreator:   cc,
		reviewChecklist: cfg.reviewChecklist,
	}

	webhookHandler := githubapp.NewEventDispatcher(
		[]githubapp.EventHandler{prCommentHandler},
		cfg.Github.App.WebhookSecret,
		githubapp.WithScheduler(
			githubapp.AsyncScheduler(),
		),
	)

	http.Handle(githubapp.DefaultWebhookRoute, webhookHandler)

	addr := cfg.address + ":8080"
	logger.Info().Msgf("Starting server on %s...", addr)
	err = http.ListenAndServe(addr, nil)
	if err != nil {
		panic(err)
	}
}
