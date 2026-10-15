#!/bin/sh
# Run from the server checkout after updating the source. The existing app data
# volume retains old content-hashed frontend chunks for tabs opened before deploy.
set -eu

compose_project=${COMPOSE_PROJECT:-mr-system-prod}
proxy_container=${1:-${PROXY_CONTAINER:-qut-proxy}}
app_container=$(docker compose -p "$compose_project" ps -q app)
if [ -z "$app_container" ]; then
  echo "No running app container; start the initial deployment with Docker Compose." >&2
  exit 1
fi
if [ "$(docker inspect --format '{{.State.Running}}' "$app_container")" != "true" ]; then
  echo "The current app must be running to preserve its frontend assets." >&2
  exit 1
fi

echo "Preserving frontend assets from the current release..."
docker exec "$app_container" sh -eu -c '
  upload_dir=${UPLOAD_DIR:-${DATA_DIR:-/app/data}/uploads}
  asset_archive="$(dirname "$upload_dir")/web-assets"
  source_assets="${WEB_DIR:-/app/web}/assets"
  test -d "$source_assets"
  mkdir -p "$asset_archive"
  cp -n "$source_assets"/* "$asset_archive"/
'

docker compose -p "$compose_project" build app
docker compose -p "$compose_project" up -d --no-build --no-deps app
app_container=$(docker compose -p "$compose_project" ps -q app)
ready=false
for attempt in 1 2 3 4 5 6 7 8 9 10 11 12; do
  if docker exec "$app_container" wget -T 3 -q -O /dev/null http://127.0.0.1:8080/api/health; then
    ready=true
    break
  fi
  sleep 5
done
if [ "$ready" != true ]; then
  echo "The rebuilt app did not become healthy. Check app logs before changing the proxy." >&2
  exit 1
fi
docker exec "$proxy_container" nginx -t
docker exec "$proxy_container" nginx -s reload
echo "App deployment complete; previous frontend assets remain available."
