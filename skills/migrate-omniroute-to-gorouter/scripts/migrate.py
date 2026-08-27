#!/usr/bin/env python3
"""One-time OmniRoute SQLite to GoRouter PostgreSQL migration."""

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
except ImportError as exc:  # pragma: no cover - environment check
    raise SystemExit("cryptography is required: python3 -m pip install cryptography") from exc


PROVIDERS = {
    "claude": "claude", "codex": "codex", "openai": "openai",
    "anthropic": "anthropic", "gemini": "gemini", "groq": "groq",
    "openrouter": "openrouter", "opencode-zen": "opencode-zen",
    "xai": "xai", "xai-oauth": "xai", "deepseek": "deepseek",
    "moonshot": "moonshot", "qwen": "qwen",
}
PREFIXES = {"codex": "cx", "opencode-zen": "ocz", "xai": "xai", "xai-oauth": "xai"}


def args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--source-db", type=Path, required=True)
    p.add_argument("--source-env", type=Path, required=True)
    p.add_argument("--target-env", type=Path, required=True)
    p.add_argument("--postgres-container", required=True)
    p.add_argument("--backup-dir", type=Path)
    p.add_argument("--apply", action="store_true", help="write after backups; default is dry-run")
    return p.parse_args()


def env_file(path: Path) -> dict[str, str]:
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


def source(path: Path) -> sqlite3.Connection:
    db = sqlite3.connect(f"file:{path.resolve()}?mode=ro", uri=True)
    db.row_factory = sqlite3.Row
    db.execute("PRAGMA query_only=ON")
    required = {"provider_connections", "model_capabilities", "usage_history"}
    found = {row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
    missing = required - found
    if missing:
        raise RuntimeError("unsupported OmniRoute schema; missing: " + ", ".join(sorted(missing)))
    return db


def psql(container: str) -> list[str]:
    return ["docker", "exec", "-i", container, "sh", "-lc", 'psql -X -v ON_ERROR_STOP=1 -qAt -U "$POSTGRES_USER" -d "$POSTGRES_DB"']


def query(container: str, sql: str) -> str:
    done = subprocess.run(psql(container), input=sql, text=True, capture_output=True, check=False)
    if done.returncode:
        raise RuntimeError("target PostgreSQL query failed")
    return done.stdout.strip()


def backup(ns: argparse.Namespace) -> None:
    if not ns.backup_dir:
        raise RuntimeError("--backup-dir is required with --apply")
    ns.backup_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    source_copy = ns.backup_dir / f"omniroute-{stamp}.sqlite"
    target_dump = ns.backup_dir / f"gorouter-{stamp}.sql"
    shutil.copy2(ns.source_db, source_copy)
    with target_dump.open("wb") as output:
        done = subprocess.run(["docker", "exec", ns.postgres_container, "sh", "-lc", 'pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"'], stdout=output, stderr=subprocess.DEVNULL, check=False)
    if done.returncode:
        target_dump.unlink(missing_ok=True)
        raise RuntimeError("target PostgreSQL backup failed")
    source_copy.chmod(0o600)
    target_dump.chmod(0o600)


def omni_key(secret: str) -> bytes:
    return hashlib.scrypt(secret.encode(), salt=b"omniroute-field-encryption-v1", n=16384, r=8, p=1, dklen=32, maxmem=64 * 1024 * 1024)


def decrypt(value: object, secret: str) -> str:
    text = str(value or "")
    if not text.startswith("enc:v1:"):
        return text
    parts = text[7:].split(":")
    if len(parts) != 3:
        raise RuntimeError("malformed OmniRoute ciphertext")
    iv, ciphertext, tag = (bytes.fromhex(part) for part in parts)
    return AESGCM(omni_key(secret)).decrypt(iv, ciphertext + tag, None).decode()


def seal(master: str, plaintext: bytes) -> bytes:
    derived = hashlib.sha256(f"gorouter:credential-encryption:{master}".encode()).digest()
    encoded = base64.urlsafe_b64encode(derived).rstrip(b"=")
    key = hashlib.sha256(encoded).digest()
    nonce = os.urandom(12)
    return nonce + AESGCM(key).encrypt(nonce, plaintext, None)


def stable(prefix: str, value: str) -> str:
    return prefix + hashlib.sha256(value.encode()).hexdigest()[:24]


def public_model(provider: str, model: str) -> str:
    mapped = PROVIDERS.get(provider, provider)
    return f"{PREFIXES.get(mapped, mapped)}/{model.lstrip('/')}"


def oauth_blob(row: sqlite3.Row, secret: str) -> bytes:
    data = {}
    try:
        data = json.loads(row["provider_specific_data"] or "{}")
    except json.JSONDecodeError:
        pass
    blob = {
        "access": decrypt(row["access_token"], secret),
        "refresh": decrypt(row["refresh_token"], secret),
        "id_token": decrypt(row["id_token"], secret),
        "account": str(data.get("workspaceId") or data.get("chatgptAccountId") or ""),
        "metadata": {"email": str(row["email"] or ""), "token_expires_at": str(row["token_expires_at"] or "")},
    }
    return json.dumps(blob, separators=(",", ":")).encode()


def collect(db: sqlite3.Connection, master: str, source_secret: str) -> tuple[list[tuple], list[tuple], list[tuple], list[str]]:
    credentials: list[tuple] = []
    connection_provider: dict[str, str] = {}
    skipped: set[str] = set()
    for row in db.execute("SELECT * FROM provider_connections ORDER BY id"):
        source_provider = str(row["provider"] or "").lower()
        provider = PROVIDERS.get(source_provider)
        if not provider:
            skipped.add(source_provider or "unknown")
            continue
        connection_provider[str(row["id"])] = provider
        kind = "oauth" if str(row["auth_type"] or "").lower() == "oauth" else "api_key"
        api_enc = oauth_enc = b""
        if kind == "oauth":
            oauth_enc = seal(master, oauth_blob(row, source_secret))
        else:
            plaintext = decrypt(row["api_key"], source_secret)
            if not plaintext:
                skipped.add(source_provider + " (empty secret)")
                continue
            api_enc = seal(master, plaintext.encode())
        credentials.append((stable("cred_omni_", str(row["id"])), str(row["name"] or row["email"] or f"Imported {provider}"), provider, kind, "", "active" if row["is_active"] else "disabled", base64.b64encode(api_enc).decode(), base64.b64encode(oauth_enc).decode(), str(row["created_at"] or datetime.now(timezone.utc).isoformat())))

    model_pairs: set[tuple[str, str, str]] = set()
    for row in db.execute("SELECT provider,model_id FROM model_capabilities WHERE status IS NULL OR status != 'disabled'"):
        provider = str(row["provider"] or "").lower()
        if provider in PROVIDERS and row["model_id"]:
            model_pairs.add((public_model(provider, str(row["model_id"])), str(row["model_id"]), PROVIDERS[provider]))
    usage: list[tuple] = []
    for row in db.execute("SELECT * FROM usage_history ORDER BY id"):
        source_provider = str(row["provider"] or "").lower()
        provider = PROVIDERS.get(source_provider)
        if not provider:
            skipped.add(source_provider or "unknown")
            continue
        model = public_model(source_provider, str(row["model"] or "unknown"))
        model_pairs.add((model, str(row["model"] or "unknown"), provider))
        cache_read = max(int(row["tokens_cache_read"] or 0), 0)
        cache_write = max(int(row["tokens_cache_creation"] or 0), 0)
        prompt = max(int(row["tokens_input"] or 0) - cache_read - cache_write, 0)
        output = max(int(row["tokens_output"] or 0), 0)
        status = str(row["status"] or "")
        code = int(status) if status.isdigit() else (200 if row["success"] else 502)
        connection_id = str(row["connection_id"] or "")
        credential = stable("cred_omni_", connection_id) if connection_id in connection_provider else ""
        usage.append((f"usage_omni_{row['id']}", str(row["timestamp"]), str(row["api_key_id"] or "omniroute-master"), credential, model, str(row["model"] or ""), prompt, output, cache_read, cache_write, code, max(int(row["latency_ms"] or 0), 0), str(row["error_code"] or "")[:512]))

    routes: list[tuple] = []
    for model, upstream, provider in sorted(model_pairs):
        matching = [item for item in credentials if item[2] == provider]
        for item in matching:
            routes.append((model, upstream, item[0]))
    return credentials, routes, usage, sorted(skipped)


def csv_text(rows: list[tuple]) -> str:
    buffer = io.StringIO()
    csv.writer(buffer, lineterminator="\n").writerows(rows)
    return buffer.getvalue()


def apply_import(container: str, credentials: list[tuple], routes: list[tuple], usage: list[tuple]) -> None:
    sql = """BEGIN;
CREATE TEMP TABLE cstage(id text,name text,provider text,kind text,base_url text,status text,api_b64 text,oauth_b64 text,created_at timestamptz) ON COMMIT DROP;
COPY cstage FROM STDIN WITH (FORMAT csv);
""" + csv_text(credentials) + "\\.\n" + """
INSERT INTO credentials(id,name,provider,kind,base_url,status,api_key_enc,oauth_blob_enc,key_preview,created_at,updated_at)
SELECT id,name,provider,kind,base_url,status,NULLIF(decode(api_b64,'base64'),'\\x'),NULLIF(decode(oauth_b64,'base64'),'\\x'),'imported',created_at,created_at FROM cstage
ON CONFLICT(id) DO NOTHING;
CREATE TEMP TABLE rstage(model text,upstream text,credential text) ON COMMIT DROP;
COPY rstage FROM STDIN WITH (FORMAT csv);
""" + csv_text(routes) + "\\.\n" + """
INSERT INTO models(name,strategy,upstream_model,enabled,created_at) SELECT DISTINCT model,'priority',upstream,true,clock_timestamp() FROM rstage ON CONFLICT(name) DO NOTHING;
INSERT INTO model_routes(model,credential_id,priority,weight,enabled) SELECT model,credential,0,1,true FROM rstage ON CONFLICT(model,credential_id) DO NOTHING;
CREATE TEMP TABLE ustage(event_id text,ts timestamptz,api_key_id text,credential_id text,model text,upstream text,prompt bigint,completion bigint,cache_read bigint,cache_write bigint,status integer,duration integer,error text) ON COMMIT DROP;
COPY ustage FROM STDIN WITH (FORMAT csv);
""" + csv_text(usage) + "\\.\n" + """
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
    raw = query(container, "SELECT count(*),coalesce(sum(prompt_tokens+cache_read_tokens+cache_write_tokens),0),coalesce(sum(completion_tokens),0),coalesce(sum(cache_read_tokens),0),coalesce(sum(cache_write_tokens),0),coalesce(sum(cost_usd),0) FROM usage_events WHERE event_id LIKE 'usage_omni_%';")
    count, input_tokens, output_tokens, cache_read, cache_write, cost = raw.split("|")
    return {"events": int(count), "input_tokens": int(input_tokens), "output_tokens": int(output_tokens), "cache_read_tokens": int(cache_read), "cache_write_tokens": int(cache_write), "cost_usd": round(float(cost), 6)}


def main() -> int:
    ns = args()
    master = env_file(ns.target_env).get("MASTER_KEY", "")
    source_secret = env_file(ns.source_env).get("STORAGE_ENCRYPTION_KEY", "")
    if not master or not source_secret:
        raise RuntimeError("required source or target encryption key is missing")
    with source(ns.source_db) as db:
        credentials, routes, usage, skipped = collect(db, master, source_secret)
        source_totals = db.execute("SELECT count(*),coalesce(sum(tokens_input),0),coalesce(sum(tokens_output),0),coalesce(sum(tokens_cache_read),0),coalesce(sum(tokens_cache_creation),0) FROM usage_history WHERE lower(coalesce(provider,'')) IN (%s)" % ",".join("?" * len(PROVIDERS)), tuple(PROVIDERS)).fetchone()
    summary: dict[str, object] = {"mode": "apply" if ns.apply else "dry-run", "credentials": len(credentials), "model_routes": len(routes), "usage_events": len(usage), "skipped_providers": skipped, "source": {"events": source_totals[0], "input_tokens": source_totals[1], "output_tokens": source_totals[2], "cache_read_tokens": source_totals[3], "cache_write_tokens": source_totals[4]}}
    if ns.apply:
        backup(ns)
        apply_import(ns.postgres_container, credentials, routes, usage)
        summary["target"] = reconcile(ns.postgres_container)
    print(json.dumps(summary, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"migration failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
