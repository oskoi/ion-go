#!/usr/bin/env bash

set -eu

export COMMIT=$(git rev-parse --short HEAD 2> /dev/null)
export BUILDTIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

export LDFLAGS="\
    -X \"github.com/oskoi/ion-go/internal.GitCommit=${COMMIT}\" \
    -X \"github.com/oskoi/ion-go/internal.BuildTime=${BUILDTIME}\" \
    ${LDFLAGS:-}"

go install --ldflags "${LDFLAGS}" ./cmd/ion-go
