#!/bin/sh
set -ex

# Setup drupal site
# /init-app.sh

# Run Nginx
exec nginx-debug -g "daemon off;"