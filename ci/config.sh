#!/bin/sh

set -euo pipefail

# Configuration script for dz-mcall Kubernetes deployment
# Usage: ./config.sh <GIT_BRANCH> <STAGING>

# Check required arguments
if [ $# -ne 2 ]; then
    echo "Usage: $0 <GIT_BRANCH> <STAGING>"
    echo "Example: $0 feature-branch dev"
    exit 1
fi

GIT_BRANCH=$1
STAGING=$2

echo "Configuring dz-mcall for branch: $GIT_BRANCH, staging: $STAGING"

# Clean up existing configuration
rm -f etc/mcall.yaml

# Set GIT_BRANCH environment variable (sanitized)
export GIT_BRANCH=$(echo "${GIT_BRANCH}" | sed 's/\//-/g')
export GIT_BRANCH=$(echo "${GIT_BRANCH}" | cut -b1-21)

echo "Sanitized GIT_BRANCH: $GIT_BRANCH"

# Replace environment variables in configuration files
if [ -n "${DEVOPS_ADMIN_PASSWORD:-}" ]; then
    echo "Updating allow_access.yaml with DEVOPS_ADMIN_PASSWORD"
    sed -i -e "s|DEVOPS_ADMIN_PASSWORD|${DEVOPS_ADMIN_PASSWORD}|" etc/allow_access.yaml
    
    echo "Updating block_access.yaml with DEVOPS_ADMIN_PASSWORD"
    sed -i -e "s|DEVOPS_ADMIN_PASSWORD|${DEVOPS_ADMIN_PASSWORD}|" etc/block_access.yaml
else
    echo "Warning: DEVOPS_ADMIN_PASSWORD not set, skipping password replacement"
fi

# Copy appropriate configuration based on branch
if [[ "${GIT_BRANCH}" == "block" ]]; then
    echo "Using block_access.yaml configuration"
    cp -f etc/block_access.yaml etc/mcall.yaml
elif [[ "${GIT_BRANCH}" == "access" ]]; then
    echo "Using allow_access.yaml configuration"
    cp -f etc/allow_access.yaml etc/mcall.yaml
else
    echo "Using default mcall.yaml configuration"
    # Keep the default mcall.yaml if it exists
    if [ ! -f etc/mcall.yaml ]; then
        echo "Warning: No configuration file selected, using default"
    fi
fi

# Display final configuration
if [ -f etc/mcall.yaml ]; then
    echo "Final configuration file (etc/mcall.yaml):"
    echo "=========================================="
    cat etc/mcall.yaml
    echo "=========================================="
else
    echo "Error: No configuration file found!"
    exit 1
fi

echo "Configuration completed successfully"
