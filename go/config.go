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
	Github githubapp.Config

	reviewChecklist string
	address         string
	logFile         string
}

func readConfig() (*config, error) {
	err := godotenv.Load()
	if err != nil {
		return nil, err
	}

	var c config
	c.Github.SetValuesFromEnv("")

	// Read SSH private key from environment and filesystem
	pathPrivateKey := os.Getenv("PRIVATE_KEY_PATH")
	if pathPrivateKey == "" {
		return nil, errors.New("no private key path found, please set the PRIVATE_KEY_PATH environment variable")
	}
	bytes, err := os.ReadFile(pathPrivateKey)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read private key file: %s", pathPrivateKey)
	}
	c.Github.App.PrivateKey = string(bytes)

	// Read the review checklist from environment and filesystem
	pathReviewChecklist := os.Getenv("REVIEW_CHECKLIST_PATH")
	if pathReviewChecklist == "" {
		return nil, errors.New("no private key path found, please set the REVIEW_CHECKLIST_PATH environment variable")
	}
	bytes, err = os.ReadFile(pathReviewChecklist)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read review checklist file: %s", pathReviewChecklist)
	}
	c.reviewChecklist = string(bytes)

	// Get server address
	serverAddress := os.Getenv("SERVER_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1"
	}
	c.address = serverAddress

	// Get log file path
	c.logFile = os.Getenv("LOG_FILE")
	return &c, nil
}
