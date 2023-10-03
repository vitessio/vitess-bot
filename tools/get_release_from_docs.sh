#!/usr/bin/env bash

set -euxo pipefail

cat config.toml | grep next | cut -d '"' -f 2 | tr -d '\n'