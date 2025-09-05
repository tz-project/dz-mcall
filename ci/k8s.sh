#!/bin/sh

# Kubernetes 배포 스크립트
set -e

# 환경 변수 설정
BUILD_NUMBER=${1:-"latest"}
GIT_BRANCH=${2:-"main"}
NAMESPACE=${3:-"default"}
ACTION=${4:-"deploy"}

# 브랜치별 namespace 설정 (main/qa는 devops, 기타는 devops-dev)
if [ "${NAMESPACE}" = "default" ]; then
    if [ "${GIT_BRANCH}" = "main" ] || [ "${GIT_BRANCH}" = "qa" ]; then
        NAMESPACE="devops"
    else
        NAMESPACE="devops-dev"
    fi
fi

echo "🔍 실행 정보:"
echo "BUILD_NUMBER: ${BUILD_NUMBER}"
echo "GIT_BRANCH: ${GIT_BRANCH}"
echo "NAMESPACE: ${NAMESPACE}"
echo "ACTION: ${ACTION}"

# 배포 함수
deploy_to_kubernetes() {
    echo "🔍 배포 정보:"
    echo "BUILD_NUMBER: ${BUILD_NUMBER}"
    echo "GIT_BRANCH: ${GIT_BRANCH}"
    echo "NAMESPACE: ${NAMESPACE}"
    
    # kubectl 다운로드 (배포 시에만)
    echo "📥 kubectl 다운로드 중..."
    wget -q https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl && chmod +x ./kubectl

    # Git 정보 확인
    echo "--- Git 정보 ---"
    git rev-parse --abbrev-ref HEAD || echo "git rev-parse 명령어 실패"



# BUILD_NUMBER가 null이면 latest로 설정
if [ -z "${BUILD_NUMBER}" ] || [ "${BUILD_NUMBER}" = "null" ]; then
    BUILD_NUMBER="latest"
fi

# GIT_BRANCH가 null이면 Git 명령어로 확인
if [ -z "${GIT_BRANCH}" ] || [ "${GIT_BRANCH}" = "null" ]; then
    echo "GIT_BRANCH가 null이므로 Git 명령어로 확인합니다"
    GIT_BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "main")
    echo "Git 명령어로 확인된 브랜치: ${GIT_BRANCH}"
fi

# origin/ 접두사 제거
GIT_BRANCH=$(echo "${GIT_BRANCH}" | sed 's|^origin/||')
echo "🔍 정리된 GIT_BRANCH: ${GIT_BRANCH}"

# 브랜치에 따른 STAGING과 도메인 설정
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

# 도메인 생성 (통합 배포)
DOMAIN="mcall${DOMAIN_SUFFIX}.drillquiz.com"

echo "✅ STAGING: ${STAGING}"
echo "✅ 생성된 도메인: ${DOMAIN}"

if [ "${STAGING}" = "qa" ]; then
    cp -Rf ci/k8s-qa.yaml ci/k8s.yaml
elif [ "${STAGING}" = "prod" ]; then
    # production 환경에서는 기본 k8s.yaml 파일 사용 (복사 불필요)
    echo "✅ Production 환경: 기본 k8s.yaml 파일 사용"
else
    cp -Rf ci/k8s-dev.yaml ci/k8s.yaml
fi

# Secret 이름 처리 (정규화된 GIT_BRANCH 사용)
SECRET_SUFFIX="${GIT_BRANCH}"
if [ "${SECRET_SUFFIX}" = "null" ]; then
    SECRET_SUFFIX="main"
fi
# / 문자를 - 로 변경
SECRET_SUFFIX=$(echo "${SECRET_SUFFIX}" | sed 's|/|-|g')
echo "Secret 이름에 사용할 SUFFIX: ${SECRET_SUFFIX}"

# 환경 변수 파일 치환 (먼저 수행)
echo "🔧 환경 변수 파일 치환 중..."
echo "🔍 치환할 도메인: ${DOMAIN}"

# 브랜치에 따른 데이터베이스 호스트 설정
if [ "${GIT_BRANCH}" = "main" ] || [ "${GIT_BRANCH}" = "qa" ]; then
    DB_HOST="devops-postgres-postgresql.devops.svc.cluster.local"
else
    DB_HOST="devops-postgres-postgresql.devops-dev.svc.cluster.local"
fi
echo "🔍 사용할 데이터베이스 호스트: ${DB_HOST}"

# 도메인 치환
sed -i "s/DOMAIN_PLACEHOLDER/${DOMAIN}/g" env
sed -i "s/DOMAIN_PLACEHOLDER/${DOMAIN}/g" package.json

# 데이터베이스 호스트 치환
sed -i "s|POSTGRES_HOST=.*|POSTGRES_HOST=${DB_HOST}|g" env

echo "✅ 환경 변수 파일 치환 완료"

# k8s.yaml 파일 치환
echo "🔧 k8s.yaml 파일 치환 중..."
echo "치환할 값들:"
echo "  BUILD_NUMBER: ${BUILD_NUMBER}"
echo "  GIT_BRANCH: ${SECRET_SUFFIX}"
echo "  STAGING: ${STAGING}"
echo "  DOMAIN: ${DOMAIN}"

# DOMAIN_PLACEHOLDER 치환
sed -i "s/DOMAIN_PLACEHOLDER/${DOMAIN}/g" ci/k8s.yaml
sed -i "s/BUILD_NUMBER_PLACEHOLDER/${BUILD_NUMBER}/g" ci/k8s.yaml
sed -i "s/STAGING/${STAGING}/g" ci/k8s.yaml
sed -i "s/GIT_BRANCH/${SECRET_SUFFIX}/g" ci/k8s.yaml

GOOGLE_OAUTH_CLIENT_SECRET=$(echo -n ${GOOGLE_OAUTH_CLIENT_SECRET} | base64)
MINIO_SECRET_KEY=$(echo -n ${MINIO_SECRET_KEY} | base64)
POSTGRES_PASSWORD=$(echo -n ${POSTGRES_PASSWORD} | base64)
OPENAI_API_KEY=$(echo -n ${OPENAI_API_KEY} | base64 -w 0)
# 한 줄로 base64 인코딩

sed -ie "s|#GOOGLE_OAUTH_CLIENT_SECRET|${GOOGLE_OAUTH_CLIENT_SECRET}|g" ci/k8s.yaml
sed -ie "s|#MINIO_SECRET_KEY|${MINIO_SECRET_KEY}|g" ci/k8s.yaml
sed -ie "s|#POSTGRES_PASSWORD|${POSTGRES_PASSWORD}|g" ci/k8s.yaml
awk -v key="$OPENAI_API_KEY" '{gsub(/#OPENAI_API_KEY/, key)}1' ci/k8s.yaml > ci/k8s.yaml.tmp && mv ci/k8s.yaml.tmp ci/k8s.yaml

cat ci/k8s.yaml

# Secret 생성 (치환된 env 파일 사용) - 수작업으로 등록된 Secret 사용
# echo "🔐 Secret 생성 중..."
# kubectl -n ${NAMESPACE} create secret generic drillquiz-secret-${SECRET_SUFFIX} --from-env-file=env --dry-run=client -o yaml | kubectl -n ${NAMESPACE} apply -f -

# ConfigMap도 함께 업데이트 (데이터베이스 호스트 설정을 위해)
echo "🔧 ConfigMap 업데이트 중..."
kubectl -n ${NAMESPACE} create configmap drillquiz-configmap-${SECRET_SUFFIX} --from-env-file=env --dry-run=client -o yaml | kubectl -n ${NAMESPACE} apply -f -



# 기존 리소스 삭제 (실패해도 계속 진행)
echo "🗑️  기존 리소스 삭제 중..."
kubectl -n ${NAMESPACE} delete -f ci/k8s.yaml || echo "삭제할 리소스가 없습니다 (정상)"

# 새 리소스 배포
echo "🚀 새 리소스 배포 중..."
kubectl -n ${NAMESPACE} apply -f ci/k8s.yaml

# 마이그레이션 실행
echo "🔄 데이터베이스 마이그레이션 실행 중..."

# 브랜치에 따른 deployment 이름 결정
if [ "${GIT_BRANCH}" = "main" ]; then
    DEPLOYMENT_NAME="drillquiz"
else
    DEPLOYMENT_NAME="drillquiz-${SECRET_SUFFIX}"
fi

echo "🔍 사용할 deployment 이름: ${DEPLOYMENT_NAME}"
kubectl -n ${NAMESPACE} rollout status deployment/${DEPLOYMENT_NAME} --timeout=300s

# Secret에서 데이터베이스 비밀번호 가져오기
echo "🔐 Secret에서 데이터베이스 비밀번호 가져오는 중..."
DB_PASSWORD=$(kubectl -n ${NAMESPACE} get secret drillquiz-secret-${SECRET_SUFFIX} -o jsonpath='{.data.POSTGRES_PASSWORD}' | base64 -d)
if [ -z "${DB_PASSWORD}" ]; then
    echo "❌ 데이터베이스 비밀번호를 가져올 수 없습니다."
    exit 1
fi
echo "✅ 데이터베이스 비밀번호 가져오기 완료"

# 브랜치에 따른 데이터베이스 호스트 설정 (마이그레이션용)
if [ "${GIT_BRANCH}" = "main" ] || [ "${GIT_BRANCH}" = "qa" ]; then
    DB_HOST="devops-postgres-postgresql.devops.svc.cluster.local"
else
    DB_HOST="devops-postgres-postgresql.devops-dev.svc.cluster.local"
fi
echo "🔍 마이그레이션용 데이터베이스 호스트: ${DB_HOST}"

# 환경 변수와 함께 마이그레이션 실행
echo "🔄 마이그레이션 실행 중..."
kubectl -n ${NAMESPACE} exec deployment/${DEPLOYMENT_NAME} -- env POSTGRES_PASSWORD="${DB_PASSWORD}" POSTGRES_HOST="${DB_HOST}" python manage.py migrate

echo "✅ 배포 및 마이그레이션 완료!"
}

# 메인 실행 로직
case "${ACTION}" in
    "deploy")
        deploy_to_kubernetes
        ;;
    *)
        echo "❌ 잘못된 ACTION: ${ACTION}"
        echo "사용법: $0 <BUILD_NUMBER> <GIT_BRANCH> <NAMESPACE> [deploy]"
        exit 1
        ;;
esac

