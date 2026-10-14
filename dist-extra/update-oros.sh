#!/bin/sh
# Downloads the latest orOS and replaces the app folder (the old one is kept as app.old).
exec "$(dirname "$0")/orOS" update
