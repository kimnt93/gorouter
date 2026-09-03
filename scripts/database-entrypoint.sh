#!/bin/sh
set -eu

connection=${DB_CONNECTION_URL:?DB_CONNECTION_URL is required}
without_scheme=${connection#*://}
authority=${without_scheme%%/*}
auth=${authority%@*}
database_and_query=${without_scheme#*/}
database=${database_and_query%%\?*}

if [ "$auth" = "$authority" ]; then
  user=""
  password=""
else
  user=${auth%%:*}
  password=${auth#*:}
fi

url_decode() {
  encoded=$(printf '%s' "$1" | sed 's/%/\\x/g')
  printf '%b' "$encoded"
}

user=$(url_decode "$user")
password=$(url_decode "$password")
database=$(url_decode "$database")

case ${1:-} in
  postgres)
    export POSTGRES_USER=${user:?PostgreSQL URL requires a user}
    export POSTGRES_PASSWORD=${password:?PostgreSQL URL requires a password}
    export POSTGRES_DB=${database:?PostgreSQL URL requires a database}
    exec docker-entrypoint.sh postgres
    ;;
  clickhouse)
    export CLICKHOUSE_USER=${user:?ClickHouse URL requires a user}
    export CLICKHOUSE_PASSWORD=${password:?ClickHouse URL requires a password}
    export CLICKHOUSE_DB=${database:?ClickHouse URL requires a database}
    exec /entrypoint.sh
    ;;
  *)
    echo "unsupported database entrypoint mode" >&2
    exit 2
    ;;
esac
