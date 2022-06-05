#!/bin/bash

set -e

podman load -i solarsnoop.image
(podman stop solarsnoop) || true

mkdir -p data

podman run --rm -d -p 8080:80 -p 8443:443 -v ./data:/var/local/data/ --name solarsnoop solarsnoop:latest \
  /usr/local/bin/solarsnoop -host solarsnoop.com -db /var/local/data/solarsnoop.sqlite -certs /var/local/data/certs/
