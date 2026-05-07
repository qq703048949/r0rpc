#!/usr/bin/env bash
set -euo pipefail

if [ "${EUID:-$(id -u)}" -ne 0 ]; then
  exec sudo -E bash "$0" "$@"
fi

if [ ! -f /etc/os-release ]; then
  echo "/etc/os-release not found. Unsupported Linux distribution." >&2
  exit 1
fi

. /etc/os-release
OS_ID="${ID:-}"
OS_LIKE="${ID_LIKE:-}"
DOCKER_RPM_REPO_BASE="${DOCKER_RPM_REPO_BASE:-https://download.docker.com/linux/centos}"
DOCKER_RPM_REPO_MIRROR_BASE="${DOCKER_RPM_REPO_MIRROR_BASE:-https://mirrors.aliyun.com/docker-ce/linux/centos}"

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

docker_ready() {
  has_cmd docker && docker compose version >/dev/null 2>&1
}

legacy_compose_ready() {
  has_cmd docker-compose
}

ensure_docker_service() {
  if has_cmd systemctl; then
    systemctl enable --now docker
  else
    service docker start
  fi
}

print_versions() {
  docker --version
  if docker compose version >/dev/null 2>&1; then
    docker compose version
  elif has_cmd docker-compose; then
    docker-compose --version
  fi
}

install_compose_for_existing_docker_rpm() {
  if has_cmd dnf; then
    dnf -y install docker-compose-plugin || dnf -y install docker-compose || true
  else
    yum -y install docker-compose-plugin || yum -y install docker-compose || true
  fi
}

install_docker_apt() {
  apt-get update
  apt-get install -y ca-certificates curl gnupg
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/${ID}/gpg -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/${ID} ${VERSION_CODENAME} stable" > /etc/apt/sources.list.d/docker.list
  apt-get update
  apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
}

write_docker_rpm_repo() {
  local repo_base="$1"
  cat > /etc/yum.repos.d/docker-ce.repo <<EOF
[docker-ce-stable]
name=Docker CE Stable - \$basearch
baseurl=${repo_base}/\$releasever/\$basearch/stable
enabled=1
gpgcheck=1
gpgkey=${repo_base}/gpg
EOF
}

prepare_docker_rpm_repo() {
  local repo_base="$1"
  write_docker_rpm_repo "$repo_base"

  if has_cmd dnf; then
    dnf clean all >/dev/null 2>&1 || true
    dnf -y makecache --disablerepo='*' --enablerepo='docker-ce-stable'
  else
    yum clean all >/dev/null 2>&1 || true
    yum -y makecache --disablerepo='*' --enablerepo='docker-ce-stable'
  fi
}

install_docker_rpm() {
  if has_cmd docker; then
    echo "docker command already exists, trying to install compose support first..."
    install_compose_for_existing_docker_rpm
    if docker_ready || legacy_compose_ready; then
      ensure_docker_service
      print_versions
      return
    fi
  fi

  if has_cmd dnf; then
    dnf -y install dnf-plugins-core
    if ! prepare_docker_rpm_repo "$DOCKER_RPM_REPO_BASE"; then
      echo "failed to reach ${DOCKER_RPM_REPO_BASE}, falling back to ${DOCKER_RPM_REPO_MIRROR_BASE}" >&2
      prepare_docker_rpm_repo "$DOCKER_RPM_REPO_MIRROR_BASE"
    fi
    dnf -y install --allowerasing docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  else
    yum -y install yum-utils
    if ! prepare_docker_rpm_repo "$DOCKER_RPM_REPO_BASE"; then
      echo "failed to reach ${DOCKER_RPM_REPO_BASE}, falling back to ${DOCKER_RPM_REPO_MIRROR_BASE}" >&2
      prepare_docker_rpm_repo "$DOCKER_RPM_REPO_MIRROR_BASE"
    fi
    yum -y install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  fi
}

case "$OS_ID" in
  ubuntu|debian)
    install_docker_apt
    ;;
  centos|rhel|rocky|almalinux|ol|opencloudos)
    install_docker_rpm
    ;;
  *)
    case "$OS_LIKE" in
      *debian*)
        install_docker_apt
        ;;
      *rhel*|*fedora*|*centos*|*opencloudos*)
        install_docker_rpm
        ;;
      *)
        if has_cmd dnf || has_cmd yum; then
          install_docker_rpm
        elif has_cmd apt-get; then
          install_docker_apt
        else
          echo "Unsupported Linux distribution: ID=$OS_ID ID_LIKE=$OS_LIKE" >&2
          exit 1
        fi
        ;;
    esac
    ;;
esac

ensure_docker_service
print_versions
