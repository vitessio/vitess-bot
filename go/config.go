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
	"os"

	"github.com/joho/godotenv"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
)

type config struct {
	Github githubapp.Config `yaml:"github"`
}

func readConfig() (*config, error) {
	err := godotenv.Load()
	if err != nil {
		return nil, err
	}

	var c config

	c.Github.SetValuesFromEnv("")

	pathPrivateKey := os.Getenv("PRIVATE_KEY_PATH")
	if pathPrivateKey == "" {
		return nil, errors.New("no private key path found, please set the PRIVATE_KEY_PATH environment variable")
	}
	bytes, err := os.ReadFile(pathPrivateKey)
	if err != nil {
		return nil, errors.Wrapf(err, "failed private key file: %s", pathPrivateKey)
	}
	c.Github.App.PrivateKey = string(bytes)
	return &c, nil
}