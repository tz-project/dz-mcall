#!/bin/sh

# Kubernetes deployment script
set -e

# Environment variable setup
BUILD_NUMBER=${1:-"latest"}
GIT_BRANCH=${2:-"main"}
NAMESPACE=${3:-"default"}
ACTION=${4:-"deploy"}

# Namespace setup by branch (main/qa use devops, others use devops-dev)
if [ "${NAMESPACE}" = "default" ]; then
    if [ "${GIT_BRANCH}" = "main" ] || [ "${GIT_BRANCH}" = "qa" ]; then
        NAMESPACE="devops"
    else
        NAMESPACE="devops-dev"
    fi
fi

echo "🔍 Execution info:"
echo "BUILD_NUMBER: ${BUILD_NUMBER}"
echo "GIT_BRANCH: ${GIT_BRANCH}"
echo "NAMESPACE: ${NAMESPACE}"
echo "ACTION: ${ACTION}"

# Deployment function
deploy_to_kubernetes() {
    echo "🔍 Deployment info:"
    echo "BUILD_NUMBER: ${BUILD_NUMBER}"
    echo "GIT_BRANCH: ${GIT_BRANCH}"
    echo "NAMESPACE: ${NAMESPACE}"
    
    # Download kubectl (only during deployment)
    echo "📥 Downloading kubectl..."
    wget -q https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl && chmod +x ./kubectl

    # Check Git information
    echo "--- Git Information ---"
    git rev-parse --abbrev-ref HEAD || echo "git rev-parse command failed"



# Set BUILD_NUMBER to latest if null
if [ -z "${BUILD_NUMBER}" ] || [ "${BUILD_NUMBER}" = "null" ]; then
    BUILD_NUMBER="latest"
fi

# Check GIT_BRANCH with Git command if null
if [ -z "${GIT_BRANCH}" ] || [ "${GIT_BRANCH}" = "null" ]; then
    echo "GIT_BRANCH is null, checking with Git command"
    GIT_BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "main")
    echo "Branch confirmed by Git command: ${GIT_BRANCH}"
fi

# Remove origin/ prefix
GIT_BRANCH=$(echo "${GIT_BRANCH}" | sed 's|^origin/||')
# Convert _ to - for Kubernetes resource naming rules
GIT_BRANCH=$(echo "${GIT_BRANCH}" | sed 's|_|-|g')
echo "🔍 Sanitized GIT_BRANCH: ${GIT_BRANCH}"

# STAGING and domain setup by branch
if [ "${GIT_BRANCH}" = "main" ]; then
    STAGING="prod"
    DOMAIN_SUFFIX=""
elif [ "${GIT_BRANCH}" = "qa" ]; then
    STAGING="qa"
    DOMAIN_SUFFIX="-qa"
else
    STAGING="dev"
    DOMAIN_SUFFIX="-${GIT_BRANCH}"
fi

# Domain generation (unified deployment)
DOMAIN="mcall${DOMAIN_SUFFIX}.drillquiz.com"

echo "✅ STAGING: ${STAGING}"
echo "✅ Generated domain: ${DOMAIN}"

if [ "${STAGING}" = "qa" ]; then
    cp -Rf ci/k8s-qa.yaml ci/k8s.yaml
elif [ "${STAGING}" = "prod" ]; then
    # Use default k8s.yaml file in production environment (no copy needed)
    echo "✅ Production environment: Using default k8s.yaml file"
else
    if [ "${GIT_BRANCH}" = "access-leader" ] || [ "${GIT_BRANCH}" = "block-leader" ]; then
      cp -Rf ci/k8s-deployment.yaml ci/k8s.yaml
    elif [ "${GIT_BRANCH}" = "access" ] || [ "${GIT_BRANCH}" = "block" ]; then
      cp -Rf ci/k8s-crontab.yaml ci/k8s.yaml
    else
      cp -Rf ci/k8s-dev.yaml ci/k8s.yaml
    fi
fi

# Secret name processing (using sanitized GIT_BRANCH)
SECRET_SUFFIX="${GIT_BRANCH}"
if [ "${SECRET_SUFFIX}" = "null" ]; then
    SECRET_SUFFIX="main"
fi
# Convert / to -
SECRET_SUFFIX=$(echo "${SECRET_SUFFIX}" | sed 's|/|-|g')
echo "SUFFIX for secret name: ${SECRET_SUFFIX}"

# Environment variable file substitution (performed first)
echo "🔧 Substituting environment variable files..."
echo "🔍 Domain to substitute: ${DOMAIN}"

echo "✅ Environment variable file substitution completed"

# k8s.yaml file substitution
echo "🔧 Substituting k8s.yaml file..."
echo "Values to substitute:"
echo "  BUILD_NUMBER: ${BUILD_NUMBER}"
echo "  GIT_BRANCH: ${SECRET_SUFFIX}"
echo "  STAGING: ${STAGING}"
echo "  DOMAIN: ${DOMAIN}"

# DOMAIN_PLACEHOLDER substitution
sed -i "s/DOMAIN_PLACEHOLDER/${DOMAIN}/g" ci/k8s.yaml
sed -i "s/BUILD_NUMBER_PLACEHOLDER/${BUILD_NUMBER}/g" ci/k8s.yaml
sed -i "s/STAGING/${STAGING}/g" ci/k8s.yaml
sed -i "s/GIT_BRANCH/${SECRET_SUFFIX}/g" ci/k8s.yaml

GOOGLE_OAUTH_CLIENT_SECRET=$(echo -n ${GOOGLE_OAUTH_CLIENT_SECRET} | base64)
MINIO_SECRET_KEY=$(echo -n ${MINIO_SECRET_KEY} | base64)
POSTGRES_PASSWORD=$(echo -n ${POSTGRES_PASSWORD} | base64)
OPENAI_API_KEY=$(echo -n ${OPENAI_API_KEY} | base64 -w 0)
# Base64 encoding in one line

sed -ie "s|#GOOGLE_OAUTH_CLIENT_SECRET|${GOOGLE_OAUTH_CLIENT_SECRET}|g" ci/k8s.yaml
sed -ie "s|#MINIO_SECRET_KEY|${MINIO_SECRET_KEY}|g" ci/k8s.yaml
sed -ie "s|#POSTGRES_PASSWORD|${POSTGRES_PASSWORD}|g" ci/k8s.yaml
awk -v key="$OPENAI_API_KEY" '{gsub(/#OPENAI_API_KEY/, key)}1' ci/k8s.yaml > ci/k8s.yaml.tmp && mv ci/k8s.yaml.tmp ci/k8s.yaml

cat ci/k8s.yaml

# Deploy RBAC resources (deploy first)
echo "🔐 Deploying RBAC resources..."
sed -i "s/STAGING/${STAGING}/g" ci/k8s-rbac.yaml
sed -i "s/GIT_BRANCH/${SECRET_SUFFIX}/g" ci/k8s-rbac.yaml
sed -i "s/NAMESPACE/${NAMESPACE}/g" ci/k8s-rbac.yaml
kubectl apply -f ci/k8s-rbac.yaml

# Delete existing resources (continue even if failed)
echo "🗑️  Deleting existing resources..."
kubectl -n ${NAMESPACE} delete -f ci/k8s.yaml || echo "No resources to delete (normal)"

# Deploy new resources
echo "🚀 Deploying new resources..."
kubectl -n ${NAMESPACE} apply -f ci/k8s.yaml
}

# Main execution logic
case "${ACTION}" in
    "deploy")
        deploy_to_kubernetes
        ;;
    *)
        echo "❌ Invalid ACTION: ${ACTION}"
        echo "Usage: $0 <BUILD_NUMBER> <GIT_BRANCH> <NAMESPACE> [deploy]"
        exit 1
        ;;
esac

