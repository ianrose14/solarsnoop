#!/bin/bash

set -e

podman load -i solarsnoop.image
(podman stop solarsnoop) || true
(podman rm solarsnoop) || true

mkdir -p data

podman run -d -p 8080:80 -p 8443:443 -v ./data:/var/local/data/  -v ./config:/var/local/config/ \
  --name solarsnoop solarsnoop:latest \
  /usr/local/bin/solarsnoop -host solarsnoop.com -db /var/local/data/solarsnoop.sqlite -certs /var/local/data/certs/ \
  -secrets /var/local/config/secrets.yaml
