#!bash
set -uexo pipefail
export DOCKER_HOST=tcp://127.0.0.1:12376
until docker version; do sleep 1; done

docker build ./2gi-build

echo "done"
