#!/bin/sh
set -eu

case "${1:-}" in
  api|worker|migrate)
    binary="/app/$1"
    shift
    exec "$binary" "$@"
    ;;
  *)
    exec "$@"
    ;;
esac
