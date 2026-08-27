#!/usr/bin/env python3
"""One-time 9Router SQLite to GoRouter PostgreSQL migration."""

from __future__ import annotations

import argparse
import base64
import csv
import hashlib
import io
import json
import os
import shutil
import sqlite3
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

try:
    from cryptography.hazmat.primitives.ciphers.aead import AESGCM
except ImportError as exc:  # pragma: no cover
    raise SystemExit("cryptography is required: python3 -m pip install cryptography") from exc


PROVIDERS = {
    "claude": "claude", "codex": "codex", "openai": "openai",
    "anthropic": "anthropic", "gemini": "gemini", "groq": "groq",
    "openrouter": "openrouter", "opencode-zen": "opencode-zen",
    "xai": "xai", "deepseek": "deepseek", "moonshot": "moonshot", "qwen": "qwen",
}
PREFIXES = {"codex": "cx", "opencode-zen": "ocz"}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-db", type=Path, required=True)
    parser.add_argument("--target-env", type=Path, required=True)
    parser.add_argument("--postgres-container", required=True)
    parser.add_argument("--backup-dir", type=Path)
    parser.add_argument("--apply", action="store_true", help="write after backups; default is dry-run")
    return parser.parse_args()


def load_env(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        value = value.strip()
        if len(value) > 1 and value[0] == value[-1] and value[0] in "\"'":
            value = value[1:-1]
        values[key.strip()] = value
    return values


def open_source(path: Path) -> sqlite3.Connection:
    db = sqlite3.connect(f"file:{path.resolve()}?mode=ro", uri=True)
    db.row_factory = sqlite3.Row
    db.execute("PRAGMA query_only=ON")
    tables = {row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
    missing = {"providerConnections", "usageHistory"} - tables
    if missing:
        raise RuntimeError("unsupported 9Router schema; missing: " + ", ".join(sorted(missing)))
    required = {
        "providerConnections": {"id", "provider", "authType", "data", "isActive"},
        "usageHistory": {"id", "timestamp", "provider", "model", "tokens"},
    }
    for table, columns in required.items():
        found = {row[1] for row in db.execute(f"PRAGMA table_info({table})")}
        if not columns <= found:
            raise RuntimeError(f"unsupported 9Router {table} columns")
    return db


def psql(container: str) -> list[str]:
    return ["docker", "exec", "-i", container, "sh", "-lc", 'psql -X -v ON_ERROR_STOP=1 -qAt -U "$POSTGRES_USER" -d "$POSTGRES_DB"']


def query(container: str, sql: str) -> str:
    done = subprocess.run(psql(container), input=sql, text=True, capture_output=True, check=False)
    if done.returncode:
        raise RuntimeError("target PostgreSQL query failed")
    return done.stdout.strip()


def backups(ns: argparse.Namespace) -> None:
    if not ns.backup_dir:
        raise RuntimeError("--backup-dir is required with --apply")
    ns.backup_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    source_copy = ns.backup_dir / f"9router-{stamp}.sqlite"
    target_dump = ns.backup_dir / f"gorouter-{stamp}.sql"
    shutil.copy2(ns.source_db, source_copy)
    with target_dump.open("wb") as output:
        done = subprocess.run(["docker", "exec", ns.postgres_container, "sh", "-lc", 'pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"'], stdout=output, stderr=subprocess.DEVNULL, check=False)
    if done.returncode:
        target_dump.unlink(missing_ok=True)
        raise RuntimeError("target PostgreSQL backup failed")
    source_copy.chmod(0o600)
    target_dump.chmod(0o600)


def seal(master: str, plaintext: bytes) -> bytes:
    derived = hashlib.sha256(f"gorouter:credential-encryption:{master}".encode()).digest()
    encoded = base64.urlsafe_b64encode(derived).rstrip(b"=")
    key = hashlib.sha256(encoded).digest()
    nonce = os.urandom(12)
    return nonce + AESGCM(key).encrypt(nonce, plaintext, None)


def stable(prefix: str, value: str) -> str:
    return prefix + hashlib.sha256(value.encode()).hexdigest()[:24]


def model_name(provider: str, model: str) -> str:
    mapped = PROVIDERS.get(provider, provider)
    return f"{PREFIXES.get(mapped, mapped)}/{model.lstrip('/')}"


def parse_json(value: object) -> dict[str, object]:
    try:
        parsed = json.loads(str(value or "{}"))
        return parsed if isinstance(parsed, dict) else {}
    except json.JSONDecodeError:
        return {}


def token(value: dict[str, object], *names: str) -> int:
    for name in names:
        if name in value:
            try:
                return max(int(value[name] or 0), 0)
            except (TypeError, ValueError):
                return 0
    return 0


def collect(db: sqlite3.Connection, master: str) -> tuple[list[tuple], list[tuple], list[tuple], list[str], dict[str, int]]:
    credentials: list[tuple] = []
    providers_by_connection: dict[str, str] = {}
    default_models: set[tuple[str, str, str]] = set()
    skipped: set[str] = set()
    for row in db.execute("SELECT * FROM providerConnections ORDER BY id"):
        source_provider = str(row["provider"] or "").lower()
        provider = PROVIDERS.get(source_provider)
        if not provider:
            skipped.add(source_provider or "unknown")
            continue
        data = parse_json(row["data"])
        secret = str(data.get("apiKey") or "")
        access = str(data.get("accessToken") or "")
        refresh = str(data.get("refreshToken") or "")
        kind = "oauth" if str(row["authType"] or "").lower() == "oauth" else "api_key"
        api_enc = oauth_enc = b""
        if kind == "oauth":
            if not access and not refresh:
                skipped.add(source_provider + " (empty OAuth secret)")
                continue
            blob = {"access": access, "refresh": refresh, "id_token": str(data.get("idToken") or ""), "account": str(data.get("accountId") or ""), "metadata": {"email": str(row["email"] or "")}}
            oauth_enc = seal(master, json.dumps(blob, separators=(",", ":")).encode())
        else:
            if not secret:
                secret = access
            if not secret:
                skipped.add(source_provider + " (empty API secret)")
                continue
            api_enc = seal(master, secret.encode())
        connection_id = str(row["id"])
        providers_by_connection[connection_id] = provider
        created = str(row["createdAt"] or datetime.now(timezone.utc).isoformat())
        credentials.append((stable("cred_9r_", connection_id), str(row["name"] or row["email"] or f"Imported {provider}"), provider, kind, "", "active" if row["isActive"] else "disabled", base64.b64encode(api_enc).decode(), base64.b64encode(oauth_enc).decode(), created))
        default_model = str(data.get("defaultModel") or "")
        if default_model:
            default_models.add((model_name(source_provider, default_model), default_model, provider))

    usage: list[tuple] = []
    models = set(default_models)
    totals = {"events": 0, "input_tokens": 0, "output_tokens": 0, "cache_read_tokens": 0, "cache_write_tokens": 0}
    for row in db.execute("SELECT * FROM usageHistory ORDER BY id"):
        source_provider = str(row["provider"] or "").lower()
        provider = PROVIDERS.get(source_provider)
        if not provider:
            skipped.add(source_provider or "unknown")
            continue
        upstream = str(row["model"] or "unknown")
        public = model_name(source_provider, upstream)
        models.add((public, upstream, provider))
        tokens = parse_json(row["tokens"])
        cache_read = token(tokens, "cached_tokens", "cache_read_input_tokens")
        cache_write = token(tokens, "cache_creation_input_tokens")
        prompt_total = token(tokens, "prompt_tokens", "input_tokens")
        prompt = max(prompt_total - cache_read - cache_write, 0)
        completion = token(tokens, "completion_tokens", "output_tokens")
        if not prompt and "promptTokens" in row.keys():
            prompt = max(int(row["promptTokens"] or 0) - cache_read - cache_write, 0)
        if not completion and "completionTokens" in row.keys():
            completion = max(int(row["completionTokens"] or 0), 0)
        status_text = str(row["status"] or "")
        status = int(status_text) if status_text.isdigit() else (200 if status_text.lower() in {"", "ok", "success"} else 502)
        connection_id = str(row["connectionId"] or "") if "connectionId" in row.keys() else ""
        credential = stable("cred_9r_", connection_id) if connection_id in providers_by_connection else ""
        usage.append((f"usage_9r_{row['id']}", str(row["timestamp"]), "9router-master", credential, public, upstream, prompt, completion, cache_read, cache_write, status, 0, ""))
        totals["events"] += 1
        totals["input_tokens"] += prompt + cache_read + cache_write
        totals["output_tokens"] += completion
        totals["cache_read_tokens"] += cache_read
        totals["cache_write_tokens"] += cache_write

    routes: list[tuple] = []
    for public, upstream, provider in sorted(models):
        for credential in credentials:
            if credential[2] == provider:
                routes.append((public, upstream, credential[0]))
    return credentials, routes, usage, sorted(skipped), totals


def csv_data(rows: list[tuple]) -> str:
    output = io.StringIO()
    csv.writer(output, lineterminator="\n").writerows(rows)
    return output.getvalue()


def import_all(container: str, credentials: list[tuple], routes: list[tuple], usage: list[tuple]) -> None:
    sql = """BEGIN;
CREATE TEMP TABLE cstage(id text,name text,provider text,kind text,base_url text,status text,api_b64 text,oauth_b64 text,created_at timestamptz) ON COMMIT DROP;
COPY cstage FROM STDIN WITH (FORMAT csv);
""" + csv_data(credentials) + "\\.\n" + """
INSERT INTO credentials(id,name,provider,kind,base_url,status,api_key_enc,oauth_blob_enc,key_preview,created_at,updated_at)
SELECT id,name,provider,kind,base_url,status,NULLIF(decode(api_b64,'base64'),'\\x'),NULLIF(decode(oauth_b64,'base64'),'\\x'),'imported',created_at,created_at FROM cstage ON CONFLICT(id) DO NOTHING;
CREATE TEMP TABLE rstage(model text,upstream text,credential text) ON COMMIT DROP;
COPY rstage FROM STDIN WITH (FORMAT csv);
""" + csv_data(routes) + "\\.\n" + """
INSERT INTO models(name,strategy,upstream_model,enabled,created_at) SELECT DISTINCT model,'priority',upstream,true,clock_timestamp() FROM rstage ON CONFLICT(name) DO NOTHING;
INSERT INTO model_routes(model,credential_id,priority,weight,enabled) SELECT model,credential,0,1,true FROM rstage ON CONFLICT(model,credential_id) DO NOTHING;
CREATE TEMP TABLE ustage(event_id text,ts timestamptz,api_key_id text,credential_id text,model text,upstream text,prompt bigint,completion bigint,cache_read bigint,cache_write bigint,status integer,duration integer,error text) ON COMMIT DROP;
COPY ustage FROM STDIN WITH (FORMAT csv);
""" + csv_data(usage) + "\\.\n" + """
INSERT INTO usage_events(event_id,ts,tenant_id,api_key_id,credential_id,model,upstream_model,prompt_tokens,completion_tokens,cache_read_tokens,cache_write_tokens,cost_usd,input_cost_usd,output_cost_usd,cache_read_cost_usd,cache_write_cost_usd,priced,cache_hit,status_code,duration_ms,error,actor_type,user_id,username,organization_id,request_body,response_body,content_truncated)
SELECT u.event_id,u.ts,'',u.api_key_id,u.credential_id,u.model,u.upstream,u.prompt,u.completion,u.cache_read,u.cache_write,
 (u.prompt*coalesce(p.input_per_m,0)+u.completion*coalesce(p.output_per_m,0)+u.cache_read*coalesce(p.cached_input_per_m,0)+u.cache_write*coalesce(p.cache_write_per_m,0))/1000000.0,
 u.prompt*coalesce(p.input_per_m,0)/1000000.0,u.completion*coalesce(p.output_per_m,0)/1000000.0,u.cache_read*coalesce(p.cached_input_per_m,0)/1000000.0,u.cache_write*coalesce(p.cache_write_per_m,0)/1000000.0,true,false,u.status,u.duration,u.error,'master','','master','','','',false
FROM ustage u LEFT JOIN prices p ON p.model=u.model ON CONFLICT(event_id) DO NOTHING;
COMMIT;
"""
    done = subprocess.run(psql(container), input=sql, text=True, capture_output=True, check=False)
    if done.returncode:
        raise RuntimeError("migration transaction failed: " + " ".join(done.stderr.splitlines()[-3:])[:500])


def reconcile(container: str) -> dict[str, object]:
    raw = query(container, "SELECT count(*),coalesce(sum(prompt_tokens+cache_read_tokens+cache_write_tokens),0),coalesce(sum(completion_tokens),0),coalesce(sum(cache_read_tokens),0),coalesce(sum(cache_write_tokens),0),coalesce(sum(cost_usd),0) FROM usage_events WHERE event_id LIKE 'usage_9r_%';")
    count, input_tokens, output_tokens, cache_read, cache_write, cost = raw.split("|")
    return {"events": int(count), "input_tokens": int(input_tokens), "output_tokens": int(output_tokens), "cache_read_tokens": int(cache_read), "cache_write_tokens": int(cache_write), "cost_usd": round(float(cost), 6)}


def main() -> int:
    ns = parse_args()
    master = load_env(ns.target_env).get("MASTER_KEY", "")
    if not master:
        raise RuntimeError("MASTER_KEY is missing from target environment")
    with open_source(ns.source_db) as db:
        credentials, routes, usage, skipped, totals = collect(db, master)
    summary: dict[str, object] = {"mode": "apply" if ns.apply else "dry-run", "credentials": len(credentials), "model_routes": len(routes), "usage_events": len(usage), "skipped_providers": skipped, "source": totals}
    if ns.apply:
        backups(ns)
        import_all(ns.postgres_container, credentials, routes, usage)
        summary["target"] = reconcile(ns.postgres_container)
    print(json.dumps(summary, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"migration failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
