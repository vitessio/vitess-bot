#!/usr/bin/env bash

cat config.toml | grep next | cut -d '"' -f 2 | tr -d '\n'