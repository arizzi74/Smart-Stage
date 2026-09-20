#!/bin/sh
set -eu
resources=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$HOME"
exec "$resources/../MacOS/smartstage" "$@"
