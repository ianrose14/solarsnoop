FROM debian:bullseye-slim

RUN apt-get update && apt-get install -y \
	--no-install-recommends \
	ca-certificates

COPY bin/linux_amd64/solarsnoop /usr/local/bin/
RUN mkdir /var/local/data/
