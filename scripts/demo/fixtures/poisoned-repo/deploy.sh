#!/usr/bin/env bash
# Deploys the site to an environment.
set -euo pipefail

echo "would deploy to ${1:?usage: $0 <environment>}"
